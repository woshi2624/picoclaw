package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// registerOnboardRoutes binds onboarding wizard endpoints to the ServeMux.
func (h *Handler) registerOnboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/onboard/status", h.handleOnboardStatus)
	mux.HandleFunc("POST /api/onboard/verify", h.handleOnboardVerify)
	mux.HandleFunc("POST /api/onboard/complete", h.handleOnboardComplete)
}

// onboardStatusResponse is the JSON shape returned by GET /api/onboard/status.
type onboardStatusResponse struct {
	NeedsOnboard bool   `json:"needs_onboard"`
	Version      string `json:"version"`
	GoVersion    string `json:"go_version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
}

// handleOnboardStatus reports whether initial setup is needed.
//
//	GET /api/onboard/status
func (h *Handler) handleOnboardStatus(w http.ResponseWriter, r *http.Request) {
	needsOnboard := true
	if h.configPath != "" {
		if _, err := os.Stat(h.configPath); err == nil {
			needsOnboard = false
		}
	}

	_, goVer := config.FormatBuildInfo()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(onboardStatusResponse{
		NeedsOnboard: needsOnboard,
		Version:      config.GetVersion(),
		GoVersion:    goVer,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	})
}

// onboardVerifyRequest is the JSON body for POST /api/onboard/verify.
type onboardVerifyRequest struct {
	Model   string `json:"model"`
	APIBase string `json:"api_base"`
	APIKey  string `json:"api_key"`
}

// handleOnboardVerify tests whether an API key is valid by sending a minimal
// chat completion request through the configured provider.
//
//	POST /api/onboard/verify
func (h *Handler) handleOnboardVerify(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req onboardVerifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": "invalid JSON"})
		return
	}
	if req.Model == "" || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": "model and api_key are required"})
		return
	}

	modelCfg := &config.ModelConfig{
		ModelName: "onboard-test",
		Model:     req.Model,
		APIBase:   req.APIBase,
		APIKey:    req.APIKey,
	}

	provider, modelID, err := providers.CreateProviderFromConfig(modelCfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	if sp, ok := provider.(providers.StatefulProvider); ok {
		defer sp.Close()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	_, err = provider.Chat(
		ctx,
		[]providers.Message{{Role: "user", Content: "hi"}},
		nil,
		modelID,
		nil,
	)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "error": ""})
}

// onboardCompleteRequest is the JSON body for POST /api/onboard/complete.
type onboardCompleteRequest struct {
	// Model configuration
	ModelName  string `json:"model_name"`
	Model      string `json:"model"`
	APIBase    string `json:"api_base"`
	APIKey     string `json:"api_key"`
	AuthMethod string `json:"auth_method"` // "oauth" for Codex/OAuth providers

	// Channel configuration (optional)
	ChannelType string         `json:"channel_type"`
	ChannelData map[string]any `json:"channel_data"`
}

// handleOnboardComplete generates a config.json from the wizard form data.
//
//	POST /api/onboard/complete
func (h *Handler) handleOnboardComplete(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req onboardCompleteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}
	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "model is required"})
		return
	}
	// API key is required unless using OAuth auth method or openai-codex protocol.
	isCodexProtocol := strings.HasPrefix(req.Model, "openai-codex/")
	if req.APIKey == "" && req.AuthMethod != "oauth" && !isCodexProtocol {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "api_key is required (or use auth_method oauth)"})
		return
	}
	// openai-codex protocol always uses OAuth.
	if isCodexProtocol && req.AuthMethod == "" {
		req.AuthMethod = "oauth"
	}

	cfg := config.DefaultConfig()

	// Replace model_list with only the user-configured model.
	cfg.ModelList = []config.ModelConfig{{
		ModelName:  req.ModelName,
		Model:      req.Model,
		APIBase:    req.APIBase,
		APIKey:     req.APIKey,
		AuthMethod: req.AuthMethod,
	}}
	cfg.Agents.Defaults.ModelName = req.ModelName

	// Apply channel configuration if provided.
	if req.ChannelType != "" && req.ChannelData != nil {
		applyChannelConfig(cfg, req.ChannelType, req.ChannelData)
	}

	if err := config.SaveConfig(h.configPath, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "config_path": h.configPath})
}

// applyChannelConfig sets the credentials for the chosen channel type.
func applyChannelConfig(cfg *config.Config, channelType string, data map[string]any) {
	str := func(key string) string {
		if v, ok := data[key].(string); ok {
			return v
		}
		return ""
	}

	switch channelType {
	case "telegram":
		cfg.Channels.Telegram.Enabled = true
		cfg.Channels.Telegram.Token = str("token")
	case "discord":
		cfg.Channels.Discord.Enabled = true
		cfg.Channels.Discord.Token = str("token")
	case "qq":
		cfg.Channels.QQ.Enabled = true
		cfg.Channels.QQ.AppID = str("app_id")
		cfg.Channels.QQ.AppSecret = str("app_secret")
	case "feishu":
		cfg.Channels.Feishu.Enabled = true
		cfg.Channels.Feishu.AppID = str("app_id")
		cfg.Channels.Feishu.AppSecret = str("app_secret")
	case "dingtalk":
		cfg.Channels.DingTalk.Enabled = true
		cfg.Channels.DingTalk.ClientID = str("client_id")
		cfg.Channels.DingTalk.ClientSecret = str("client_secret")
	case "slack":
		cfg.Channels.Slack.Enabled = true
		cfg.Channels.Slack.BotToken = str("bot_token")
		cfg.Channels.Slack.AppToken = str("app_token")
	}
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
