// redact.go — slog.Handler wrapper that replaces secret attribute values
// with "[redacted]" before forwarding the record to the underlying handler.
//
// The set of secret attribute keys is determined by the caller (typically
// derived via reflection from internal/config.Config's `secret:"true"` tags)
// to avoid an import cycle between internal/log and internal/config.
//
// Requirements: MVP-FND-4.5. Design: §7 (redaction wrapper).
package log

import (
	"context"
	"log/slog"
)

const redactedValue = "[redacted]"

// redactingHandler wraps an inner slog.Handler and replaces the value of any
// attribute whose key is in the secrets set with "[redacted]".
type redactingHandler struct {
	inner   slog.Handler
	secrets map[string]struct{}
}

// Enabled delegates to the inner handler.
func (h *redactingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

// Handle scans the record's attributes, replaces secret values, then forwards
// the modified record to the inner handler.
func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Collect and redact attributes.
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		if _, isSecret := h.secrets[a.Key]; isSecret {
			a = slog.String(a.Key, redactedValue)
		}
		attrs = append(attrs, a)
		return true
	})

	// Build a new record without the original attributes.
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	newRecord.AddAttrs(attrs...)

	return h.inner.Handle(ctx, newRecord)
}

// WithAttrs returns a new handler with the given attributes pre-set, applying
// redaction to any secret keys in the attribute list.
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		if _, isSecret := h.secrets[a.Key]; isSecret {
			a = slog.String(a.Key, redactedValue)
		}
		redacted[i] = a
	}
	return &redactingHandler{
		inner:   h.inner.WithAttrs(redacted),
		secrets: h.secrets,
	}
}

// WithGroup returns a new handler with the given group name applied.
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{
		inner:   h.inner.WithGroup(name),
		secrets: h.secrets,
	}
}
