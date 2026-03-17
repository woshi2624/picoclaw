package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type SendCallback func(channel, chatID, content string) error

type MessageTool struct {
	sendCallback  SendCallback
	sentInRound   sync.Map // chatID → bool; tracks per-chat send state for concurrent processing
	mirrorChannel sync.Map // chatID → "channel:chatID"; when set, mirrors sends to a second channel
}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a message to user on a chat channel. Use this when you want to communicate something."
}

func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The message content to send",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Optional: target channel (telegram, whatsapp, etc.)",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Optional: target chat/user ID",
			},
		},
		"required": []string{"content"},
	}
}

// ResetSentInRound resets the per-chat send tracker for the given chatID.
// Called by the agent loop at the start of each inbound message processing round.
func (t *MessageTool) ResetSentInRound(chatID string) {
	t.sentInRound.Delete(chatID)
	t.mirrorChannel.Delete(chatID)
}

// SetMirrorChannel registers a secondary "channel:chatID" destination for sends from the given chatID.
// When set, every successful send from chatID is also mirrored there.
// Pass an empty string to clear.
func (t *MessageTool) SetMirrorChannel(chatID, mirrorTo string) {
	if mirrorTo == "" {
		t.mirrorChannel.Delete(chatID)
	} else {
		t.mirrorChannel.Store(chatID, mirrorTo)
	}
}

// HasSentInRound returns true if the message tool sent a message for the given chatID
// during the current processing round.
func (t *MessageTool) HasSentInRound(chatID string) bool {
	v, ok := t.sentInRound.Load(chatID)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{ForLLM: "content is required", IsError: true}
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if channel == "" {
		channel = ToolChannel(ctx)
	}
	if chatID == "" {
		chatID = ToolChatID(ctx)
	}

	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified", IsError: true}
	}

	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Message sending not configured", IsError: true}
	}

	if err := t.sendCallback(channel, chatID, content); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending message: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	t.sentInRound.Store(chatID, true)

	// Mirror send to secondary channel if configured (e.g., pico → feishu).
	if v, ok := t.mirrorChannel.Load(chatID); ok {
		if mirrorTo, ok := v.(string); ok && mirrorTo != "" {
			if idx := strings.Index(mirrorTo, ":"); idx > 0 {
				mirrorCh := mirrorTo[:idx]
				mirrorID := mirrorTo[idx+1:]
				if mirrorCh != channel {
					_ = t.sendCallback(mirrorCh, mirrorID, content)
				}
			}
		}
	}

	// Silent: user already received the message directly
	return &ToolResult{
		ForLLM: fmt.Sprintf("Message sent to %s:%s", channel, chatID),
		Silent: true,
	}
}
