// Package log_test tests slog-based logger initialisation and redaction.
// Requirements: MVP-FND-4.1 through MVP-FND-4.3, MVP-FND-4.6. Design: §7.
package log_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	lgblog "github.com/fgjcarlos/lgb/internal/log"
)

// TestJSONFormatEmitsValidJSON asserts MVP-FND-4.3: json format produces valid
// JSON objects per line.
func TestJSONFormatEmitsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger, err := lgblog.New(lgblog.Options{
		Level:  "info",
		Format: "json",
		Out:    &buf,
	})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	logger.Info("hello", "component", "test")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no output written to buffer")
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Errorf("log output is not valid JSON: %v\noutput: %s", err, line)
	}
	if obj["msg"] != "hello" {
		t.Errorf("json msg = %v; want %q", obj["msg"], "hello")
	}
}

// TestTextFormatEmitsKeyValue asserts MVP-FND-4.3: text format produces
// human-readable key=value lines.
func TestTextFormatEmitsKeyValue(t *testing.T) {
	var buf bytes.Buffer
	logger, err := lgblog.New(lgblog.Options{
		Level:  "info",
		Format: "text",
		Out:    &buf,
	})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	logger.Info("world", "component", "test")

	line := buf.String()
	if !strings.Contains(line, "msg=world") {
		t.Errorf("text output missing msg=world; got: %s", line)
	}
	if !strings.Contains(line, "component=test") {
		t.Errorf("text output missing component=test; got: %s", line)
	}
}

// TestInvalidLevelReturnsError asserts MVP-FND-4.2: invalid level strings
// return an error instead of panicking.
func TestInvalidLevelReturnsError(t *testing.T) {
	_, err := lgblog.New(lgblog.Options{
		Level:  "verbose",
		Format: "text",
		Out:    &bytes.Buffer{},
	})
	if err == nil {
		t.Error("New() with invalid level returned nil error; want error")
	}
}

// TestInvalidFormatReturnsError asserts MVP-FND-4.3: invalid format strings
// return an error instead of panicking.
func TestInvalidFormatReturnsError(t *testing.T) {
	_, err := lgblog.New(lgblog.Options{
		Level:  "info",
		Format: "xml",
		Out:    &bytes.Buffer{},
	})
	if err == nil {
		t.Error("New() with invalid format returned nil error; want error")
	}
}

// TestConcurrentLoggingNoRace asserts MVP-FND-4.6: concurrent goroutines can
// log without data races. This test is only meaningful when run with -race.
func TestConcurrentLoggingNoRace(t *testing.T) {
	var buf safeBuffer
	logger, err := lgblog.New(lgblog.Options{
		Level:  "info",
		Format: "json",
		Out:    &buf,
	})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			logger.Info("concurrent", "goroutine", i)
		}()
	}
	wg.Wait()

	// Verify we got some output — each goroutine wrote at least one line.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < goroutines {
		t.Errorf("expected at least %d log lines; got %d", goroutines, len(lines))
	}
}

// TestDebugSuppressedAtInfoLevel asserts MVP-FND-4.2: DEBUG messages are not
// emitted when the level is INFO.
func TestDebugSuppressedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	logger, err := lgblog.New(lgblog.Options{
		Level:  "info",
		Format: "text",
		Out:    &buf,
	})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	logger.Debug("should not appear")
	logger.Info("should appear")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Error("DEBUG message appeared with INFO level; want suppressed")
	}
	if !strings.Contains(output, "should appear") {
		t.Error("INFO message did not appear; want visible")
	}
}

// TestDebugLevelEmitsDebugMessages triangulates MVP-FND-4.2: at DEBUG level,
// debug messages ARE emitted.
func TestDebugLevelEmitsDebugMessages(t *testing.T) {
	var buf bytes.Buffer
	logger, err := lgblog.New(lgblog.Options{
		Level:  "debug",
		Format: "text",
		Out:    &buf,
	})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	logger.Debug("debug message")

	if !strings.Contains(buf.String(), "debug message") {
		t.Error("DEBUG message not in output at DEBUG level; want visible")
	}
}

// TestSourceAttachedOnlyAtDebug asserts design §7: AddSource is true only at
// DEBUG level. At INFO, the source location should NOT appear.
func TestSourceAttachedOnlyAtDebug(t *testing.T) {
	var infoBuf, debugBuf bytes.Buffer

	infoLogger, _ := lgblog.New(lgblog.Options{Level: "info", Format: "text", Out: &infoBuf})
	debugLogger, _ := lgblog.New(lgblog.Options{Level: "debug", Format: "text", Out: &debugBuf})

	infoLogger.Info("info msg")
	debugLogger.Debug("debug msg")

	infoOutput := infoBuf.String()
	debugOutput := debugBuf.String()

	// At INFO level, source should NOT be present.
	if strings.Contains(infoOutput, "source=") || strings.Contains(infoOutput, ".go:") {
		t.Errorf("INFO logger emits source location; want none; got: %s", infoOutput)
	}
	// At DEBUG level, source SHOULD be present (AddSource: true).
	if !strings.Contains(debugOutput, ".go:") {
		t.Errorf("DEBUG logger does not emit source location; want it; got: %s", debugOutput)
	}
}

// safeBuffer is a bytes.Buffer safe for concurrent writes.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Ensure safeBuffer implements io.Writer for slog handler.
var _ interface{ Write([]byte) (int, error) } = (*safeBuffer)(nil)

// TestRedactionHandlerReplacesSecretValues asserts MVP-FND-4.5 and design §7:
// the redacting handler replaces values for keys in the secret set.
func TestRedactionHandlerReplacesSecretValues(t *testing.T) {
	var buf bytes.Buffer

	secretKeys := []string{"jwtSecret", "password"}
	logger, err := lgblog.NewWithRedaction(lgblog.Options{
		Level:  "info",
		Format: "json",
		Out:    &buf,
	}, secretKeys)
	if err != nil {
		t.Fatalf("NewWithRedaction() returned error: %v", err)
	}

	logger.Info("config loaded",
		slog.String("jwtSecret", "redaction-fixture-001"),
		slog.String("component", "config"),
	)

	output := buf.String()
	if strings.Contains(output, "redaction-fixture-001") {
		t.Errorf("secret value leaked into log; output: %s", output)
	}
	if !strings.Contains(output, "[redacted]") {
		t.Errorf("expected [redacted] in output; got: %s", output)
	}
}
