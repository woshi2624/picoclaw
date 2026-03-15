package anthropicprovider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
)

// parseAnthropicSSE reads an Anthropic SSE streaming response and accumulates
// the events into an LLMResponse, matching the behaviour of the SDK accumulator.
// onToken is called for each text_delta chunk; pass nil to disable streaming callbacks.
func parseAnthropicSSE(r io.Reader, onToken func(delta, accumulated string)) (*LLMResponse, error) {
	type toolState struct {
		id    string
		name  string
		input strings.Builder
	}

	var (
		content      strings.Builder
		reasoning    strings.Builder
		toolStates   = map[int]*toolState{}
		stopReason   string
		inputTokens  int
		outputTokens int
	)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<17), 1<<17) // 128 KB – handles large tool inputs

	var dataStr string

	dispatch := func() {
		if dataStr == "" || dataStr == "[DONE]" {
			return
		}
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(dataStr), &base); err != nil {
			return // skip malformed lines
		}

		switch base.Type {
		case "message_start":
			var e struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			json.Unmarshal([]byte(dataStr), &e) //nolint:errcheck
			inputTokens = e.Message.Usage.InputTokens

		case "content_block_start":
			var e struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			json.Unmarshal([]byte(dataStr), &e) //nolint:errcheck
			if e.ContentBlock.Type == "tool_use" {
				toolStates[e.Index] = &toolState{
					id:   e.ContentBlock.ID,
					name: e.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			var e struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			json.Unmarshal([]byte(dataStr), &e) //nolint:errcheck
			switch e.Delta.Type {
			case "text_delta":
				content.WriteString(e.Delta.Text)
				if onToken != nil {
					onToken(e.Delta.Text, content.String())
				}
			case "thinking_delta":
				reasoning.WriteString(e.Delta.Thinking)
			case "input_json_delta":
				if ts, ok := toolStates[e.Index]; ok {
					ts.input.WriteString(e.Delta.PartialJSON)
				}
			}

		case "message_delta":
			var e struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			json.Unmarshal([]byte(dataStr), &e) //nolint:errcheck
			if e.Delta.StopReason != "" {
				stopReason = e.Delta.StopReason
			}
			outputTokens = e.Usage.OutputTokens
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			dispatch()
			dataStr = ""
			continue
		}
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			dataStr = after
		}
	}
	dispatch() // handle last event when there is no trailing blank line

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}

	// Build tool calls sorted by block index so ordering is deterministic.
	var toolCalls []ToolCall
	if len(toolStates) > 0 {
		indices := make([]int, 0, len(toolStates))
		for idx := range toolStates {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		for _, idx := range indices {
			ts := toolStates[idx]
			var args map[string]any
			if err := json.Unmarshal([]byte(ts.input.String()), &args); err != nil {
				log.Printf("anthropic: failed to decode tool call input for %q: %v", ts.name, err)
				args = map[string]any{"raw": ts.input.String()}
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        ts.id,
				Name:      ts.name,
				Arguments: args,
			})
		}
	}

	finishReason := "stop"
	switch stopReason {
	case "tool_use":
		finishReason = "tool_calls"
	case "max_tokens":
		finishReason = "length"
	}

	return &LLMResponse{
		Content:      content.String(),
		Reasoning:    reasoning.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: &UsageInfo{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
	}, nil
}
