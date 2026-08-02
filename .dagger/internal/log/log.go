// Package log defines dagmar's structured-logging port: a Logger interface (KB
// guide.golang.logging) implemented over the standard library's log/slog — the KB's
// documented Go-1.26 minimal-dependency alternative — and injected via constructors
// (ADR-0010 §7/§8). slog keeps the Dagger module dependency-light.
package log

import (
	"log/slog"
	"os"
)

// Logger is dagmar's logging port. Every service that logs receives this via constructor
// injection (ADR-0010 §7), enabling test substitution without a DI container.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// slogLogger adapts the stdlib slog.Logger to the Logger port.
type slogLogger struct{ l *slog.Logger }

// New returns a Logger backed by slog writing JSON to stdout (Dagger captures stdout).
func New() Logger {
	return &slogLogger{l: slog.New(slog.NewJSONHandler(os.Stdout, nil))}
}

func (s *slogLogger) Debug(msg string, args ...any) { s.l.Debug(msg, args...) }
func (s *slogLogger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s *slogLogger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s *slogLogger) Error(msg string, args ...any) { s.l.Error(msg, args...) }
