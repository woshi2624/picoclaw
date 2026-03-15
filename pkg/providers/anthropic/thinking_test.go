package anthropicprovider

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestApplyThinkingConfig_Adaptive(t *testing.T) {
	params := anthropic.MessageNewParams{
		MaxTokens:   16000,
		Temperature: anthropic.Float(0.7),
	}
	applyThinkingConfig(&params, "adaptive")

	if params.Thinking.OfAdaptive == nil {
		t.Fatal("expected adaptive thinking")
	}
	if params.Thinking.OfEnabled != nil {
		t.Error("should not set enabled thinking in adaptive mode")
	}
	if params.OutputConfig.Effort != anthropic.OutputConfigEffortHigh {
		t.Errorf("effort = %q, want %q", params.OutputConfig.Effort, anthropic.OutputConfigEffortHigh)
	}
	if params.Temperature.Valid() {
		t.Error("temperature should be cleared when thinking is enabled")
	}
}

func TestApplyThinkingConfig_BudgetLevels(t *testing.T) {
	tests := []struct {
		level      string
		wantBudget int64
	}{
		{"low", 4096},
		{"medium", 16384},
		{"high", 32000},
		{"xhigh", 64000},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			params := anthropic.MessageNewParams{
				MaxTokens:   200000,
				Temperature: anthropic.Float(0.5),
			}
			applyThinkingConfig(&params, tt.level)

			if params.Thinking.OfEnabled == nil {
				t.Fatal("expected enabled thinking")
			}
			if params.Thinking.OfAdaptive != nil {
				t.Error("should not set adaptive thinking")
			}
			if params.Thinking.OfEnabled.BudgetTokens != tt.wantBudget {
				t.Errorf("budget_tokens = %d, want %d", params.Thinking.OfEnabled.BudgetTokens, tt.wantBudget)
			}
			if params.OutputConfig.Effort != "" {
				t.Errorf("effort = %q, want empty", params.OutputConfig.Effort)
			}
			if params.Temperature.Valid() {
				t.Error("temperature should be cleared when thinking is enabled")
			}
		})
	}
}

func TestApplyThinkingConfig_BudgetClamp(t *testing.T) {
	// budget_tokens must be < max_tokens; clamp budget down to respect user's max_tokens.
	params := anthropic.MessageNewParams{MaxTokens: 4096}
	applyThinkingConfig(&params, "high") // budget=32000 > maxTokens=4096

	if params.Thinking.OfEnabled == nil {
		t.Fatal("expected enabled thinking")
	}
	if params.Thinking.OfEnabled.BudgetTokens != 4095 {
		t.Errorf("budget_tokens = %d, want 4095 (maxTokens-1)", params.Thinking.OfEnabled.BudgetTokens)
	}
	if params.MaxTokens != 4096 {
		t.Errorf("max_tokens should not be modified, got %d", params.MaxTokens)
	}
}

func TestApplyThinkingConfig_UnknownLevel(t *testing.T) {
	params := anthropic.MessageNewParams{MaxTokens: 16000}
	applyThinkingConfig(&params, "unknown")

	if params.Thinking.OfEnabled != nil {
		t.Error("should not set enabled thinking for unknown level")
	}
	if params.Thinking.OfAdaptive != nil {
		t.Error("should not set adaptive thinking for unknown level")
	}
}

func TestLevelToBudget(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  int
	}{
		{"low", "low", 4096},
		{"medium", "medium", 16384},
		{"high", "high", 32000},
		{"xhigh", "xhigh", 64000},
		{"off", "off", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := levelToBudget(tt.level); got != tt.want {
				t.Errorf("levelToBudget(%q) = %d, want %d", tt.level, got, tt.want)
			}
		})
	}
}

func TestBuildParams_ThinkingClearsTemperature(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	opts := map[string]any{
		"max_tokens":     200000,
		"temperature":    0.8,
		"thinking_level": "medium",
	}

	params, err := buildParams(msgs, nil, "claude-sonnet-4-6", opts)
	if err != nil {
		t.Fatal(err)
	}

	if params.Temperature.Valid() {
		t.Error("temperature should be cleared when thinking_level is set")
	}
	if params.Thinking.OfEnabled == nil {
		t.Fatal("expected enabled thinking")
	}
	if params.Thinking.OfEnabled.BudgetTokens != 16384 {
		t.Errorf("budget_tokens = %d, want 16384", params.Thinking.OfEnabled.BudgetTokens)
	}
}

// unmarshalBlocks is no longer used; thinking response parsing is tested via parseAnthropicSSE.

func TestParseResponse_ThinkingBlock(t *testing.T) {
	// Verify that thinking_delta events populate the Reasoning field.
	sse := strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Let me reason step by step...\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"The answer is 42.\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	)

	result, err := parseAnthropicSSE(sse, nil)
	if err != nil {
		t.Fatalf("parseAnthropicSSE error: %v", err)
	}
	if result.Reasoning != "Let me reason step by step..." {
		t.Errorf("Reasoning = %q, want thinking content", result.Reasoning)
	}
	if result.Content != "The answer is 42." {
		t.Errorf("Content = %q, want text content", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", result.FinishReason)
	}
}

func TestParseResponse_NoThinkingBlock(t *testing.T) {
	sse := strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Just a normal response.\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	)

	result, err := parseAnthropicSSE(sse, nil)
	if err != nil {
		t.Fatalf("parseAnthropicSSE error: %v", err)
	}
	if result.Reasoning != "" {
		t.Errorf("Reasoning = %q, want empty", result.Reasoning)
	}
	if result.Content != "Just a normal response." {
		t.Errorf("Content = %q, want text content", result.Content)
	}
}

func TestBuildParams_NoThinkingKeepsTemperature(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	opts := map[string]any{
		"temperature": 0.8,
	}

	params, err := buildParams(msgs, nil, "claude-sonnet-4-6", opts)
	if err != nil {
		t.Fatal(err)
	}

	if !params.Temperature.Valid() {
		t.Error("temperature should be preserved when thinking is not set")
	}
	if params.Temperature.Value != 0.8 {
		t.Errorf("temperature = %f, want 0.8", params.Temperature.Value)
	}
}
