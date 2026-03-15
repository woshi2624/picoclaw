package anthropicprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

type (
	ToolCall               = protocoltypes.ToolCall
	FunctionCall           = protocoltypes.FunctionCall
	LLMResponse            = protocoltypes.LLMResponse
	UsageInfo              = protocoltypes.UsageInfo
	Message                = protocoltypes.Message
	ContentBlock           = protocoltypes.ContentBlock
	CacheControl           = protocoltypes.CacheControl
	ToolDefinition         = protocoltypes.ToolDefinition
	ToolFunctionDefinition = protocoltypes.ToolFunctionDefinition
)

const (
	defaultBaseURL      = "https://api.anthropic.com"
	anthropicBetaHeader = "oauth-2025-04-20"
)

// retryDelayUnit controls the base delay between retries. Override in tests.
var retryDelayUnit = time.Second

type Provider struct {
	tokenSource func() (string, error)
	baseURL     string
	apiKey      string
}

// SupportsThinking implements providers.ThinkingCapable.
func (p *Provider) SupportsThinking() bool { return true }

func NewProvider(token string) *Provider {
	return NewProviderWithBaseURL(token, "")
}

func NewProviderWithBaseURL(token, apiBase string) *Provider {
	return &Provider{
		baseURL: normalizeBaseURL(apiBase),
		apiKey:  token,
	}
}

func NewProviderWithTokenSource(token string, tokenSource func() (string, error)) *Provider {
	return NewProviderWithTokenSourceAndBaseURL(token, tokenSource, "")
}

func NewProviderWithTokenSourceAndBaseURL(token string, tokenSource func() (string, error), apiBase string) *Provider {
	return &Provider{
		tokenSource: tokenSource,
		baseURL:     normalizeBaseURL(apiBase),
		apiKey:      token,
	}
}

func (p *Provider) Chat(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (*LLMResponse, error) {
	params, err := buildParams(messages, tools, model, options)
	if err != nil {
		return nil, err
	}

	if p.tokenSource != nil {
		// Token is fetched fresh per-attempt inside doHTTPStreaming; log placeholder.
		logAnthropicRequest(p.baseURL, params, true, requestHeadersForAuthToken(""))
	} else {
		logAnthropicRequest(p.baseURL, params, true, requestHeadersForAPIKey(p.apiKey))
	}

	return p.chatStreamingWithCallback(ctx, params, nil)
}

// ChatStream implements providers.StreamingCapable.
// It calls the LLM with streaming enabled and invokes onToken for each text delta.
func (p *Provider) ChatStream(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
	onToken protocoltypes.TokenCallback,
) (*LLMResponse, error) {
	params, err := buildParams(messages, tools, model, options)
	if err != nil {
		return nil, err
	}

	if p.tokenSource != nil {
		logAnthropicRequest(p.baseURL, params, true, requestHeadersForAuthToken(""))
	} else {
		logAnthropicRequest(p.baseURL, params, true, requestHeadersForAPIKey(p.apiKey))
	}

	return p.chatStreamingWithCallback(ctx, params, onToken)
}

func (p *Provider) chatStreamingWithCallback(
	ctx context.Context,
	params anthropic.MessageNewParams,
	onToken protocoltypes.TokenCallback,
) (*LLMResponse, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := range maxAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelayUnit * time.Duration(attempt)):
			}
		}

		resp, err := p.doHTTPStreaming(ctx, params, onToken)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isProxyRetryable(err) {
			break
		}
		log.Printf("anthropic: retrying after transient error (attempt %d/%d): %v", attempt+1, maxAttempts, err)
	}
	return nil, fmt.Errorf("claude API call: %w", lastErr)
}

// doHTTPStreaming sends a raw HTTP POST that mirrors the curl command:
//
//	curl -H 'content-type: application/json' \
//	     -H 'x-api-key: <key>' \
//	     -H 'anthropic-version: 2023-06-01' \
//	     -d '<body>' \
//	     https://<base>/v1/messages
//
// This avoids SDK-injected headers (X-Stainless-*, User-Agent, etc.) that some
// proxy relays reject with 403.
func (p *Provider) doHTTPStreaming(ctx context.Context, params anthropic.MessageNewParams, onToken protocoltypes.TokenCallback) (*LLMResponse, error) {
	bodyStr, err := marshalAnthropicRequestBody(params, true)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := strings.TrimRight(p.baseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(bodyStr))
	if err != nil {
		return nil, err
	}

	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if p.tokenSource != nil {
		tok, err := p.tokenSource()
		if err != nil {
			return nil, fmt.Errorf("refreshing token: %w", err)
		}
		req.Header.Set("authorization", "Bearer "+tok)
		req.Header.Set("anthropic-beta", anthropicBetaHeader)
	} else {
		req.Header.Set("x-api-key", p.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &proxyHTTPError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}

	return parseAnthropicSSE(resp.Body, onToken)
}

// proxyHTTPError is returned when the relay/proxy responds with a non-200 status.
type proxyHTTPError struct {
	StatusCode int
	Body       string
}

func (e *proxyHTTPError) Error() string {
	return fmt.Sprintf("%d %s: %s", e.StatusCode, http.StatusText(e.StatusCode), e.Body)
}

func isProxyRetryable(err error) bool {
	var httpErr *proxyHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusForbidden ||
			httpErr.StatusCode == http.StatusTooManyRequests ||
			httpErr.StatusCode >= 500
	}
	return false
}

func (p *Provider) GetDefaultModel() string {
	return "claude-sonnet-4.6"
}

func (p *Provider) BaseURL() string {
	return p.baseURL
}

func buildParams(
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (anthropic.MessageNewParams, error) {
	var system []anthropic.TextBlockParam
	var anthropicMessages []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// Prefer structured SystemParts for per-block cache_control.
			// This enables LLM-side KV cache reuse: the static block's prefix
			// hash stays stable across requests while dynamic parts change freely.
			if len(msg.SystemParts) > 0 {
				for _, part := range msg.SystemParts {
					system = append(system, anthropic.TextBlockParam{Text: part.Text})
				}
			} else {
				system = append(system, anthropic.TextBlockParam{Text: msg.Content})
			}
		case "user":
			if msg.ToolCallID != "" {
				anthropicMessages = append(anthropicMessages,
					anthropic.NewUserMessage(anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false)),
				)
			} else {
				anthropicMessages = append(anthropicMessages,
					anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)),
				)
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var blocks []anthropic.ContentBlockParamUnion
				if msg.Content != "" {
					blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
				}
				for _, tc := range msg.ToolCalls {
					toolName := tc.Name
					if toolName == "" && tc.Function != nil {
						toolName = tc.Function.Name
					}
					if toolName == "" {
						logger.WarnCF("provider.anthropic", "Skipping tool call with empty name in history", map[string]any{
							"tool_call_id": tc.ID,
						})
						continue
					}
					args := tc.Arguments
					if args == nil && tc.Function != nil && tc.Function.Arguments != "" {
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
							args = map[string]any{}
						}
					}
					if args == nil {
						args = map[string]any{}
					}
					blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, args, toolName))
				}
				anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(blocks...))
			} else {
				anthropicMessages = append(anthropicMessages,
					anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)),
				)
			}
		case "tool":
			anthropicMessages = append(anthropicMessages,
				anthropic.NewUserMessage(anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false)),
			)
		}
	}

	maxTokens := int64(4096)
	if mt, ok := options["max_tokens"].(int); ok {
		maxTokens = int64(mt)
	}

	// Normalize model ID: Anthropic API uses hyphens (claude-sonnet-4-6),
	// but config may use dots (claude-sonnet-4.6).
	apiModel := strings.ReplaceAll(model, ".", "-")

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(apiModel),
		Messages:  anthropicMessages,
		MaxTokens: maxTokens,
	}

	if len(system) > 0 {
		params.System = system
	}

	if temp, ok := options["temperature"].(float64); ok {
		params.Temperature = anthropic.Float(temp)
	}

	if len(tools) > 0 {
		params.Tools = translateTools(tools)
	}

	// Extended Thinking / Adaptive Thinking
	// The thinking_level value directly determines the API parameter format:
	//   "adaptive" → {thinking: {type: "adaptive"}} + output_config.effort
	//   "low/medium/high/xhigh" → {thinking: {type: "enabled", budget_tokens: N}}
	if level, ok := options["thinking_level"].(string); ok && level != "" && level != "off" {
		applyThinkingConfig(&params, level)
	}

	return params, nil
}

// applyThinkingConfig sets thinking parameters based on the level value.
// "adaptive" uses the adaptive thinking API (Claude 4.6+).
// All other levels use budget_tokens which is universally supported.
//
// Anthropic API constraint: temperature must not be set when thinking is enabled.
// budget_tokens must be strictly less than max_tokens.
func applyThinkingConfig(params *anthropic.MessageNewParams, level string) {
	// Anthropic API rejects requests with temperature set alongside thinking.
	// Reset to zero value (omitted from JSON serialization).
	if params.Temperature.Valid() {
		log.Printf("anthropic: temperature cleared because thinking is enabled (level=%s)", level)
	}
	params.Temperature = anthropic.MessageNewParams{}.Temperature

	if level == "adaptive" {
		adaptive := anthropic.NewThinkingConfigAdaptiveParam()
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
		params.OutputConfig = anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortHigh,
		}
		return
	}

	budget := int64(levelToBudget(level))
	if budget <= 0 {
		return
	}

	// budget_tokens must be < max_tokens; clamp to respect user's max_tokens setting.
	if budget >= params.MaxTokens {
		log.Printf("anthropic: budget_tokens (%d) clamped to %d (max_tokens-1)", budget, params.MaxTokens-1)
		budget = params.MaxTokens - 1
	} else if budget > params.MaxTokens*80/100 {
		log.Printf("anthropic: thinking budget (%d) exceeds 80%% of max_tokens (%d), output may be truncated",
			budget, params.MaxTokens)
	}
	params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
}

// levelToBudget maps a thinking level to budget_tokens.
// Values are based on Anthropic's recommendations and community best practices:
//
//	low    =  4,096  — simple reasoning, quick debugging (Claude Code "think")
//	medium = 16,384  — Anthropic recommended sweet spot for most tasks
//	high   = 32,000  — complex architecture, deep analysis (diminishing returns above this)
//	xhigh  = 64,000  — extreme reasoning, research problems, benchmarks
//
// Note: For Claude 4.6+, prefer adaptive thinking over manual budget_tokens.
func levelToBudget(level string) int {
	switch level {
	case "low":
		return 4096
	case "medium":
		return 16384
	case "high":
		return 32000
	case "xhigh":
		return 64000
	default:
		return 0
	}
}

func translateTools(tools []ToolDefinition) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		tool := anthropic.ToolParam{
			Name: t.Function.Name,
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: t.Function.Parameters["properties"],
			},
		}
		if desc := t.Function.Description; desc != "" {
			tool.Description = anthropic.String(desc)
		}
		if req, ok := t.Function.Parameters["required"].([]any); ok {
			required := make([]string, 0, len(req))
			for _, r := range req {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
			tool.InputSchema.Required = required
		}
		result = append(result, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return result
}

func normalizeBaseURL(apiBase string) string {
	base := strings.TrimSpace(apiBase)
	if base == "" {
		return defaultBaseURL
	}

	base = strings.TrimRight(base, "/")
	if before, ok := strings.CutSuffix(base, "/v1"); ok {
		base = before
	}
	if base == "" {
		return defaultBaseURL
	}

	return base
}

func logAnthropicRequest(baseURL string, params anthropic.MessageNewParams, streaming bool, headers map[string]string) {
	body, err := marshalAnthropicRequestBody(params, streaming)
	if err != nil {
		body = fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}

	logger.InfoCF("provider.anthropic", "Sending Anthropic request", map[string]any{
		"url":     strings.TrimRight(baseURL, "/") + "/v1/messages",
		"stream":  streaming,
		"headers": headers,
		"body":    body,
	})
}

func marshalAnthropicRequestBody(params anthropic.MessageNewParams, streaming bool) (string, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	if !streaming {
		return string(raw), nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	payload["stream"] = true
	raw, err = json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func requestHeadersForAPIKey(apiKey string) map[string]string {
	return map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
		"X-Api-Key":         maskSecret(apiKey),
	}
}

func requestHeadersForAuthToken(token string) map[string]string {
	return map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
		"anthropic-beta":    anthropicBetaHeader,
		"Authorization":     "Bearer " + maskSecret(token),
	}
}

func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}
