package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type AuditEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Username  string    `json:"username,omitempty"`
	TargetID  int64     `json:"target_id,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	IP        string    `json:"ip,omitempty"`
}

type AuditLogger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

func OpenAuditLogger(dir string) (*AuditLogger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	return &AuditLogger{file: f, enc: json.NewEncoder(f)}, nil
}

func (a *AuditLogger) Log(event AuditEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.enc.Encode(event)
}

func (a *AuditLogger) Close() error {
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}
