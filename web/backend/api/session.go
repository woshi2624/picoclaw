package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// registerSessionRoutes binds session list and detail endpoints to the ServeMux.
func (h *Handler) registerSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions", h.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}/events", h.handleSessionEvents)
	mux.HandleFunc("GET /api/sessions/{id}", h.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", h.handleDeleteSession)
}

// sessionMeta mirrors the on-disk .meta.json structure from pkg/memory.JSONLStore.
type sessionMeta struct {
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Skip      int       `json:"skip"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// sessionListItem is a lightweight summary returned by GET /api/sessions.
type sessionListItem struct {
	ID           string `json:"id"`
	Channel      string `json:"channel"`
	Preview      string `json:"preview"`
	MessageCount int    `json:"message_count"`
	Created      string `json:"created"`
	Updated      string `json:"updated"`
}

// picoSessionPrefix is the key prefix used by the gateway's routing for Pico
// channel sessions. The full key format is:
//
//	agent:main:pico:direct:pico:<session-uuid>
//
// The sanitized filename replaces ':' with '_', so on disk it becomes:
//
//	agent_main_pico_direct_pico_<session-uuid>.meta.json  (metadata)
//	agent_main_pico_direct_pico_<session-uuid>.jsonl      (messages)
const picoSessionPrefix = "agent:main:pico:direct:pico:"

// extractPicoSessionID extracts the session UUID from a full session key.
// Returns the UUID and true if the key matches the Pico session pattern.
func extractPicoSessionID(key string) (string, bool) {
	if strings.HasPrefix(key, picoSessionPrefix) {
		return strings.TrimPrefix(key, picoSessionPrefix), true
	}
	return "", false
}

// sessionIDFromKey returns the session ID used in the API.
// For Pico sessions the short UUID is returned for WebSocket compatibility.
// For all other channels the full session key is returned.
func sessionIDFromKey(key string) string {
	if uuid, ok := extractPicoSessionID(key); ok {
		return uuid
	}
	return key
}

// extractChannelFromKey returns the channel name from a session key.
// Session keys follow the format: agent:<agentID>:<channel>:<rest>
func extractChannelFromKey(key string) string {
	parts := strings.SplitN(key, ":", 4)
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// sessionKeyFromID resolves the full on-disk session key from an API session ID.
// If the ID contains a colon it is already a full key; otherwise it is treated
// as a Pico UUID and the picoSessionPrefix is prepended.
func sessionKeyFromID(id string) string {
	if strings.Contains(id, ":") {
		return id
	}
	return picoSessionPrefix + id
}

// sanitizeSessionKey converts a session key to the filename base used on disk.
// Mirrors pkg/memory.sanitizeKey.
func sanitizeSessionKey(key string) string {
	s := strings.ReplaceAll(key, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// sessionsDir resolves the path to the gateway's session storage directory.
// It reads the workspace from config, falling back to ~/.picoclaw/workspace.
func (h *Handler) sessionsDir() (string, error) {
	workspace, err := h.workspaceDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(workspace, "sessions"), nil
}

// workspaceDir resolves the agent workspace directory from config,
// falling back to ~/.picoclaw/workspace.
func (h *Handler) workspaceDir() (string, error) {
	cfg, err := config.LoadConfig(h.configPath)
	if err != nil {
		return "", err
	}

	workspace := cfg.Agents.Defaults.Workspace
	if workspace == "" {
		home, _ := os.UserHomeDir()
		workspace = filepath.Join(home, ".picoclaw", "workspace")
	}

	// Expand ~ prefix
	if len(workspace) > 0 && workspace[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(workspace) > 1 && workspace[1] == '/' {
			workspace = home + workspace[1:]
		} else {
			workspace = home
		}
	}

	return workspace, nil
}

// readJSONLMessages reads user/assistant messages from a .jsonl file,
// skipping the first `skip` lines (logically truncated entries).
func readJSONLMessages(jsonlPath string, skip int) ([]providers.Message, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var msgs []providers.Message
	lineNum := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10 MB max line
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineNum++
		if lineNum <= skip {
			continue
		}
		var msg providers.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs, scanner.Err()
}

// handleListSessions returns a list of Pico session summaries.
//
//	GET /api/sessions
func (h *Handler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	dir, err := h.sessionsDir()
	if err != nil {
		http.Error(w, "failed to resolve sessions directory", http.StatusInternalServerError)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist yet = no sessions
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]sessionListItem{})
		return
	}

	items := []sessionListItem{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Only process .meta.json files
		if !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		// Skip the internal heartbeat pseudo-session.
		if meta.Key == "heartbeat" {
			continue
		}

		// Derive a stable API ID and channel name from the session key.
		// Pico sessions keep the short UUID; all other channels use the full key.
		sessionID := sessionIDFromKey(meta.Key)
		channel := extractChannelFromKey(meta.Key)

		// Read messages from the .jsonl file for preview and count
		base := strings.TrimSuffix(entry.Name(), ".meta.json")
		jsonlPath := filepath.Join(dir, base+".jsonl")
		msgs, _ := readJSONLMessages(jsonlPath, meta.Skip)

		// Build a preview from the first user message
		preview := ""
		validMessageCount := 0
		for _, msg := range msgs {
			if msg.Role == "user" || msg.Role == "assistant" {
				if strings.TrimSpace(msg.Content) == "" {
					continue
				}
				validMessageCount++
				if preview == "" && msg.Role == "user" {
					preview = msg.Content
				}
			}
		}
		if len([]rune(preview)) > 60 {
			preview = string([]rune(preview)[:60]) + "..."
		}
		if preview == "" {
			preview = "(empty)"
		}

		items = append(items, sessionListItem{
			ID:           sessionID,
			Channel:      channel,
			Preview:      preview,
			MessageCount: validMessageCount,
			Created:      meta.CreatedAt.Format(time.RFC3339),
			Updated:      meta.UpdatedAt.Format(time.RFC3339),
		})
	}

	// Sort by updated descending (most recent first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Updated > items[j].Updated
	})

	// Pagination parameters
	offsetStr := r.URL.Query().Get("offset")
	limitStr := r.URL.Query().Get("limit")

	offset := 0
	limit := 20 // Default limit

	if val, err := strconv.Atoi(offsetStr); err == nil && val >= 0 {
		offset = val
	}
	if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
		limit = val
	}

	totalItems := len(items)

	end := offset + limit
	if offset >= totalItems {
		items = []sessionListItem{} // Out of bounds, return empty
	} else {
		if end > totalItems {
			end = totalItems
		}
		items = items[offset:end]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// handleGetSession returns the full message history for a specific session.
//
//	GET /api/sessions/{id}
func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	dir, err := h.sessionsDir()
	if err != nil {
		http.Error(w, "failed to resolve sessions directory", http.StatusInternalServerError)
		return
	}

	// Reconstruct file base from session key
	base := sanitizeSessionKey(sessionKeyFromID(sessionID))
	metaPath := filepath.Join(dir, base+".meta.json")
	jsonlPath := filepath.Join(dir, base+".jsonl")

	// Read metadata
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var meta sessionMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		http.Error(w, "failed to parse session metadata", http.StatusInternalServerError)
		return
	}

	// Read messages from .jsonl, respecting the skip offset
	allMsgs, err := readJSONLMessages(jsonlPath, meta.Skip)
	if err != nil {
		http.Error(w, "failed to read session messages", http.StatusInternalServerError)
		return
	}

	// Convert to a simpler format for the frontend — user and assistant only
	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	messages := make([]chatMessage, 0, len(allMsgs))
	for _, msg := range allMsgs {
		if (msg.Role == "user" || msg.Role == "assistant") && strings.TrimSpace(msg.Content) != "" {
			messages = append(messages, chatMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":       sessionID,
		"messages": messages,
		"summary":  meta.Summary,
		"created":  meta.CreatedAt.Format(time.RFC3339),
		"updated":  meta.UpdatedAt.Format(time.RFC3339),
	})
}

// handleSessionEvents streams new messages for a session via Server-Sent Events.
// The frontend passes ?skip=N to start after N already-loaded messages.
// A "typing" custom event is pushed when the session tail is a user message
// (agent is still processing) and cleared when the assistant reply arrives.
//
//	GET /api/sessions/{id}/events?skip=N
func (h *Handler) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	skipStr := r.URL.Query().Get("skip")
	seenCount := 0
	if n, err := strconv.Atoi(skipStr); err == nil && n > 0 {
		seenCount = n
	}

	dir, err := h.sessionsDir()
	if err != nil {
		http.Error(w, "failed to resolve sessions directory", http.StatusInternalServerError)
		return
	}

	base := sanitizeSessionKey(sessionKeyFromID(sessionID))
	metaPath := filepath.Join(dir, base+".meta.json")
	jsonlPath := filepath.Join(dir, base+".jsonl")

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	var meta sessionMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		http.Error(w, "failed to parse session metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	type sessionMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	prevTyping := false

	poll := func() {
		msgs, err := readJSONLMessages(jsonlPath, meta.Skip)
		if err != nil {
			return
		}

		var valid []sessionMessage
		for _, msg := range msgs {
			if (msg.Role == "user" || msg.Role == "assistant") && strings.TrimSpace(msg.Content) != "" {
				valid = append(valid, sessionMessage{Role: msg.Role, Content: msg.Content})
			}
		}

		// Push any new messages
		flushed := false
		for i := seenCount; i < len(valid); i++ {
			data, merr := json.Marshal(valid[i])
			if merr != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flushed = true
		}
		if len(valid) > seenCount {
			seenCount = len(valid)
		}

		// Push typing state change when it flips
		isTyping := len(valid) > 0 && valid[len(valid)-1].Role == "user"
		if isTyping != prevTyping {
			prevTyping = isTyping
			typingVal := "false"
			if isTyping {
				typingVal = "true"
			}
			fmt.Fprintf(w, "event: typing\ndata: %s\n\n", typingVal)
			flushed = true
		}

		if flushed {
			flusher.Flush()
		}
	}

	// Send initial delta immediately (handles messages that arrived between
	// getSessionHistory and the SSE subscription being established).
	poll()

	ticker := time.NewTicker(time.Second)
	keepalive := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case <-ticker.C:
			poll()
		}
	}
}

// handleDeleteSession deletes a specific session (both .jsonl and .meta.json files).
//
//	DELETE /api/sessions/{id}
func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	dir, err := h.sessionsDir()
	if err != nil {
		http.Error(w, "failed to resolve sessions directory", http.StatusInternalServerError)
		return
	}

	base := sanitizeSessionKey(sessionKeyFromID(sessionID))
	metaPath := filepath.Join(dir, base+".meta.json")
	jsonlPath := filepath.Join(dir, base+".jsonl")

	// The .meta.json must exist; .jsonl may not exist for very new sessions
	if err := os.Remove(metaPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to delete session", http.StatusInternalServerError)
		}
		return
	}

	// Best-effort removal of the .jsonl file
	_ = os.Remove(jsonlPath)

	w.WriteHeader(http.StatusNoContent)
}
