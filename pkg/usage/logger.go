package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageEntry represents a single LLM usage record.
type UsageEntry struct {
	Timestamp        string `json:"ts"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	SessionKey       string `json:"session,omitempty"`
	AgentID          string `json:"agent_id,omitempty"`
}

// Logger writes append-only JSONL usage records to disk.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewLogger creates a Logger that appends to {workspacePath}/usage.jsonl.
func NewLogger(workspacePath string) (*Logger, error) {
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(
		filepath.Join(workspacePath, "usage.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Log writes an entry. It is nil-safe (no-op if l is nil).
func (l *Logger) Log(entry UsageEntry) {
	if l == nil {
		return
	}
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(entry)
}

// Close flushes and closes the underlying file.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
