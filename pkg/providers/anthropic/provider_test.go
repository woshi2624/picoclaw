package anthropicprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestBuildParams_BasicMessage(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Hello"},
	}
	params, err := buildParams(messages, nil, "claude-sonnet-4.6", map[string]any{
		"max_tokens": 1024,
	})
	if err != nil {
		t.Fatalf("buildParams() error: %v", err)
	}
	if string(params.Model) != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", params.Model, "claude-sonnet-4-6")
	}
	if params.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", params.MaxTokens)
	}
	if len(params.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(params.Messages))
	}
}

func TestBuildParams_SystemMessage(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hi"},
	}
	params, err := buildParams(messages, nil, "claude-sonnet-4.6", map[string]any{})
	if err != nil {
		t.Fatalf("buildParams() error: %v", err)
	}
	if len(params.System) != 1 {
		t.Fatalf("len(System) = %d, want 1", len(params.System))
	}
	if params.System[0].Text != "You are helpful" {
		t.Errorf("System[0].Text = %q, want %q", params.System[0].Text, "You are helpful")
	}
	if len(params.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(params.Messages))
	}
}

func TestBuildParams_ToolCallMessage(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "What's the weather?"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []ToolCall{
				{
					ID:        "call_1",
					Name:      "get_weather",
					Arguments: map[string]any{"city": "SF"},
				},
			},
		},
		{Role: "tool", Content: `{"temp": 72}`, ToolCallID: "call_1"},
	}
	params, err := buildParams(messages, nil, "claude-sonnet-4.6", map[string]any{})
	if err != nil {
		t.Fatalf("buildParams() error: %v", err)
	}
	if len(params.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(params.Messages))
	}
}

func TestBuildParams_WithTools(t *testing.T) {
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:        "get_weather",
				Description: "Get weather for a city",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
		},
	}
	params, err := buildParams([]Message{{Role: "user", Content: "Hi"}}, tools, "claude-sonnet-4.6", map[string]any{})
	if err != nil {
		t.Fatalf("buildParams() error: %v", err)
	}
	if len(params.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(params.Tools))
	}
}

// sseEvents returns a minimal well-formed Anthropic SSE stream for "ok" text response.
func sseEvents(model, text string, inputTokens, outputTokens int) []string {
	return []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"" + model + "\",\"stop_reason\":null,\"usage\":{\"input_tokens\":" + itoa(inputTokens) + ",\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + text + "\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":" + itoa(outputTokens) + "}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func writeSSE(w http.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for _, e := range events {
		w.Write([]byte(e)) //nolint:errcheck
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func TestProvider_ChatRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			http.Error(w, "unexpected authorization header", http.StatusUnauthorized)
			return
		}
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["stream"] != true {
			http.Error(w, "stream required", http.StatusBadRequest)
			return
		}
		writeSSE(w, sseEvents("claude-sonnet-4-6", "Hello! How can I help you?", 15, 8))
	}))
	defer server.Close()

	provider := NewProviderWithBaseURL("test-token", server.URL)
	messages := []Message{{Role: "user", Content: "Hello"}}
	resp, err := provider.Chat(t.Context(), messages, nil, "claude-sonnet-4.6", map[string]any{"max_tokens": 1024})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "Hello! How can I help you?" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello! How can I help you?")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage.PromptTokens != 15 {
		t.Errorf("PromptTokens = %d, want 15", resp.Usage.PromptTokens)
	}
}

func TestProvider_GetDefaultModel(t *testing.T) {
	p := NewProvider("test-token")
	if got := p.GetDefaultModel(); got != "claude-sonnet-4.6" {
		t.Errorf("GetDefaultModel() = %q, want %q", got, "claude-sonnet-4.6")
	}
}

func TestProvider_NewProviderWithBaseURL_NormalizesV1Suffix(t *testing.T) {
	p := NewProviderWithBaseURL("token", "https://api.anthropic.com/v1/")
	if got := p.BaseURL(); got != "https://api.anthropic.com" {
		t.Fatalf("BaseURL() = %q, want %q", got, "https://api.anthropic.com")
	}
}

func TestProvider_ChatUsesTokenSource(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		atomic.AddInt32(&requests, 1)

		if got := r.Header.Get("Authorization"); got != "Bearer refreshed-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		writeSSE(w, sseEvents("claude-sonnet-4-6", "ok", 1, 1))
	}))
	defer server.Close()

	p := NewProviderWithTokenSourceAndBaseURL("stale-token", func() (string, error) {
		return "refreshed-token", nil
	}, server.URL)

	_, err := p.Chat(
		t.Context(),
		[]Message{{Role: "user", Content: "hello"}},
		nil,
		"claude-sonnet-4.6",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestProvider_ChatRoundTrip_IgnoresEnvAuthTokenInAPIKeyMode(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-auth-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://env.example.com")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-token" {
			http.Error(w, "bad api key", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			http.Error(w, "unexpected authorization header", http.StatusUnauthorized)
			return
		}
		writeSSE(w, sseEvents("claude-sonnet-4-6", "ok", 1, 1))
	}))
	defer server.Close()

	p := NewProviderWithBaseURL("test-token", server.URL)
	_, err := p.Chat(
		t.Context(),
		[]Message{{Role: "user", Content: "hello"}},
		nil,
		"claude-sonnet-4.6",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
}

func TestProvider_ChatStreamingRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer refreshed-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer refreshed-token")
		}
		if got := r.Header.Get("Anthropic-Beta"); got != anthropicBetaHeader {
			t.Errorf("Anthropic-Beta = %q, want %q", got, anthropicBetaHeader)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-6\",\"stop_reason\":null,\"usage\":{\"input_tokens\":12,\"output_tokens\":0}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, e := range events {
			w.Write([]byte(e)) //nolint:errcheck
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	p := NewProviderWithTokenSourceAndBaseURL("stale-token", func() (string, error) {
		return "refreshed-token", nil
	}, server.URL)

	resp, err := p.Chat(
		t.Context(),
		[]Message{{Role: "user", Content: "Hello"}},
		nil,
		"claude-sonnet-4.6",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello world")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", resp.Usage.CompletionTokens)
	}
}

func TestProvider_ChatAPIKeyUsesStreaming(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		atomic.AddInt32(&requests, 1)
		if got := r.Header.Get("X-Api-Key"); got != "test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if reqBody["stream"] != true {
			http.Error(w, "stream required", http.StatusBadRequest)
			return
		}

		writeSSE(w, sseEvents("claude-opus-4-6", "Hello", 12, 5))
	}))
	defer server.Close()

	p := NewProviderWithBaseURL("test-token", server.URL)
	resp, err := p.Chat(
		t.Context(),
		[]Message{{Role: "user", Content: "Hello"}},
		nil,
		"claude-opus-4.6",
		map[string]any{"max_tokens": 32768},
	)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "Hello" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestProvider_ChatProxyCompatibilityRequestShape(t *testing.T) {
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:        "read_file",
				Description: "Read a file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []any{"path"},
				},
			},
		},
	}

	messages := []Message{
		{
			Role: "system",
			SystemParts: []ContentBlock{
				{
					Type:         "text",
					Text:         "static prompt",
					CacheControl: &CacheControl{Type: "ephemeral"},
				},
				{
					Type: "text",
					Text: "dynamic prompt",
				},
			},
		},
		{Role: "user", Content: "你好"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		if reqBody["stream"] != true {
			http.Error(w, "stream required", http.StatusBadRequest)
			return
		}
		if got := int(reqBody["max_tokens"].(float64)); got != 4096 {
			http.Error(w, "max_tokens must be 4096", http.StatusBadRequest)
			return
		}

		rawTools, ok := reqBody["tools"].([]any)
		if !ok || len(rawTools) != 1 {
			http.Error(w, "tools required", http.StatusBadRequest)
			return
		}

		rawSystem, ok := reqBody["system"].([]any)
		if !ok || len(rawSystem) == 0 {
			http.Error(w, "system required", http.StatusBadRequest)
			return
		}
		for _, item := range rawSystem {
			block, ok := item.(map[string]any)
			if !ok {
				http.Error(w, "bad system block", http.StatusBadRequest)
				return
			}
			if _, has := block["cache_control"]; has {
				http.Error(w, "cache_control must be omitted", http.StatusBadRequest)
				return
			}
		}

		writeSSE(w, sseEvents("claude-sonnet-4-6", "ok", 12, 2))
	}))
	defer server.Close()

	p := NewProviderWithBaseURL("test-token", server.URL)
	_, err := p.Chat(
		t.Context(),
		messages,
		tools,
		"claude-sonnet-4.6",
		map[string]any{"max_tokens": 4096},
	)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
}

func TestProvider_ChatProxyCompatibilityWithoutTools(t *testing.T) {
	messages := []Message{
		{
			Role: "system",
			SystemParts: []ContentBlock{
				{
					Type:         "text",
					Text:         "static prompt",
					CacheControl: &CacheControl{Type: "ephemeral"},
				},
				{
					Type: "text",
					Text: "dynamic prompt",
				},
			},
		},
		{Role: "user", Content: "你好"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		if reqBody["stream"] != true {
			http.Error(w, "stream required", http.StatusBadRequest)
			return
		}
		if got := int(reqBody["max_tokens"].(float64)); got != 4096 {
			http.Error(w, "max_tokens must be 4096", http.StatusBadRequest)
			return
		}
		if _, ok := reqBody["tools"]; ok {
			http.Error(w, "tools must be omitted", http.StatusBadRequest)
			return
		}

		rawSystem, ok := reqBody["system"].([]any)
		if !ok || len(rawSystem) == 0 {
			http.Error(w, "system required", http.StatusBadRequest)
			return
		}
		for _, item := range rawSystem {
			block, ok := item.(map[string]any)
			if !ok {
				http.Error(w, "bad system block", http.StatusBadRequest)
				return
			}
			if _, has := block["cache_control"]; has {
				http.Error(w, "cache_control must be omitted", http.StatusBadRequest)
				return
			}
		}

		writeSSE(w, sseEvents("claude-sonnet-4-6", "ok", 12, 2))
	}))
	defer server.Close()

	p := NewProviderWithBaseURL("test-token", server.URL)
	_, err := p.Chat(
		t.Context(),
		messages,
		nil,
		"claude-sonnet-4.6",
		map[string]any{"max_tokens": 4096},
	)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
}

func TestProvider_Chat_RetryOn403(t *testing.T) {
	t.Cleanup(func() { retryDelayUnit = time.Second })
	retryDelayUnit = 0

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			// Simulate transient 403 from proxy on first two attempts.
			http.Error(w, `{"type":"error","error":{"type":"permission_error","message":"proxy transient"}}`, http.StatusForbidden)
			return
		}
		writeSSE(w, sseEvents("claude-sonnet-4-6", "ok", 5, 1))
	}))
	defer server.Close()

	p := NewProviderWithBaseURL("test-token", server.URL)
	resp, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, "claude-sonnet-4.6", map[string]any{})
	if err != nil {
		t.Fatalf("Chat() error after retry: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestProvider_Chat_NoRetryOnPermanent403(t *testing.T) {
	t.Cleanup(func() { retryDelayUnit = time.Second })
	retryDelayUnit = 0

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		atomic.AddInt32(&attempts, 1)
		http.Error(w, `{"type":"error","error":{"type":"permission_error","message":"bad key"}}`, http.StatusForbidden)
	}))
	defer server.Close()

	p := NewProviderWithBaseURL("test-token", server.URL)
	_, err := p.Chat(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil, "claude-sonnet-4.6", map[string]any{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3 (all retries exhausted)", got)
	}
}

// Ensure the anthropic SDK type alias is still accessible (compile-time check).
var _ = anthropic.StopReasonEndTurn
