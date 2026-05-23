// Package log initialises the structured logger for the LGB gateway.
//
// It wraps log/slog (stdlib) with two additions:
//  1. Configurable level (debug|info|warn|error) and format (text|json)
//  2. An optional redacting handler that replaces secret-tagged attribute values
//     with "[redacted]" before forwarding records to the underlying handler.
//
// The secret key set is provided by the caller (internal/config resolves it
// from struct tags) so this package has no import dependency on internal/config.
//
// Usage:
//
//	logger, err := log.New(log.Options{Level: "info", Format: "json", Out: os.Stderr})
//
// Requirements: MVP-FND-4.1, MVP-FND-4.2, MVP-FND-4.3. Design: §7.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Options controls logger initialisation.
type Options struct {
	// Level is one of "debug", "info", "warn", "error" (case-insensitive).
	// Default: "info".
	Level string

	// Format is one of "text" or "json" (case-insensitive).
	// Default: "text".
	Format string

	// Out is the destination writer. When nil, the caller should pass os.Stderr.
	Out io.Writer
}

// New creates a *slog.Logger from opts.
//
// Returns an error for invalid Level or Format strings per MVP-FND-4.2 and MVP-FND-4.3.
// The logger has AddSource: true only at DEBUG level (design §7).
func New(opts Options) (*slog.Logger, error) {
	h, err := buildHandler(opts)
	if err != nil {
		return nil, err
	}
	return slog.New(h), nil
}

// NewWithRedaction creates a *slog.Logger whose handler replaces values for
// keys in secretKeys with the literal "[redacted]" before logging.
//
// secretKeys should be derived from the `secret:"true"` struct tags on
// internal/config.Config (caller resolves them to avoid import cycles).
// Requirements: MVP-FND-4.5. Design: §7.
func NewWithRedaction(opts Options, secretKeys []string) (*slog.Logger, error) {
	inner, err := buildHandler(opts)
	if err != nil {
		return nil, err
	}
	keySet := make(map[string]struct{}, len(secretKeys))
	for _, k := range secretKeys {
		keySet[k] = struct{}{}
	}
	return slog.New(&redactingHandler{inner: inner, secrets: keySet}), nil
}

// buildHandler constructs a slog.Handler from opts without the redaction wrapper.
func buildHandler(opts Options) (slog.Handler, error) {
	lvl, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: lvl == slog.LevelDebug,
	}

	switch strings.ToLower(opts.Format) {
	case "json":
		return slog.NewJSONHandler(opts.Out, handlerOpts), nil
	case "text", "":
		return slog.NewTextHandler(opts.Out, handlerOpts), nil
	default:
		return nil, fmt.Errorf("log: invalid format %q: must be one of text|json", opts.Format)
	}
}

// parseLevel converts a string level name to slog.Level.
func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("log: invalid level %q: must be one of debug|info|warn|error", s)
	}
}
