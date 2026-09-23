// Package logging holds the server's log setup.
package logging

import (
	"context"
	"io"
	"log/slog"
)

// NewSplit returns a text logger that writes Debug/Info records to out and Warn/Error records to
// errOut. A Docker log line carries no priority, so the stream is the only level a log collector
// (the agent among them) sees: stdout is info, stderr is a warning (docs/SPEC.md §4h).
func NewSplit(out, errOut io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	return slog.New(split{info: slog.NewTextHandler(out, opts), warn: slog.NewTextHandler(errOut, opts)})
}

type split struct{ info, warn slog.Handler }

func (h split) pick(l slog.Level) slog.Handler {
	if l >= slog.LevelWarn {
		return h.warn
	}
	return h.info
}

func (h split) Enabled(ctx context.Context, l slog.Level) bool { return h.pick(l).Enabled(ctx, l) }

func (h split) Handle(ctx context.Context, r slog.Record) error {
	return h.pick(r.Level).Handle(ctx, r)
}

func (h split) WithAttrs(attrs []slog.Attr) slog.Handler {
	return split{info: h.info.WithAttrs(attrs), warn: h.warn.WithAttrs(attrs)}
}

func (h split) WithGroup(name string) slog.Handler {
	return split{info: h.info.WithGroup(name), warn: h.warn.WithGroup(name)}
}
