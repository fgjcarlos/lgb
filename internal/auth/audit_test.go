package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditLogger_LogAndRead(t *testing.T) {
	dir := t.TempDir()
	logger, err := OpenAuditLogger(dir)
	if err != nil {
		t.Fatalf("OpenAuditLogger: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	event := AuditEvent{
		Timestamp: now,
		Action:    "login",
		Username:  "alice",
		IP:        "192.168.1.1",
	}
	if err := logger.Log(event); err != nil {
		t.Fatalf("Log: %v", err)
	}

	event2 := AuditEvent{
		Timestamp: now,
		Action:    "user.create",
		Username:  "admin",
		TargetID:  2,
		Detail:    "created user bob",
	}
	if err := logger.Log(event2); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var parsed AuditEvent
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}
	if parsed.Action != "login" || parsed.Username != "alice" {
		t.Errorf("unexpected event: %+v", parsed)
	}
}

func TestAuditLogger_AutoTimestamp(t *testing.T) {
	dir := t.TempDir()
	logger, _ := OpenAuditLogger(dir)
	defer logger.Close()

	before := time.Now().UTC()
	_ = logger.Log(AuditEvent{Action: "test"})
	after := time.Now().UTC()

	data, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	var parsed AuditEvent
	_ = json.Unmarshal(data, &parsed)

	if parsed.Timestamp.Before(before) || parsed.Timestamp.After(after) {
		t.Errorf("auto timestamp %v not between %v and %v", parsed.Timestamp, before, after)
	}
}
