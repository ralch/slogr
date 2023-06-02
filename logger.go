package slogr

import (
	"context"
	"reflect"

	"golang.org/x/exp/slog"
)

// LoggerKey represents the context key of the logger.
var LoggerKey = &ContextKey{
	name: reflect.TypeOf(ContextKey{}).PkgPath(),
}

// ContextKey represents a context key.
type ContextKey struct {
	name string
}

// String returns the context key as a string.
func (k *ContextKey) String() string {
	return k.name
}

// FromContext returns the logger from a given context.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(LoggerKey).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}

// WithContext provides the logger in a given context.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, logger)
}

var _ slog.Leveler = LevelVar("")

// StringLevel represents a slog.Leveler for string
type LevelVar string

// Set set the value.
func (v *LevelVar) Set(value string) {
	*v = LevelVar(value)
}

// String returns the level as string.
func (v LevelVar) String() string {
	return string(v)
}

// Level implements [slog.Leveler].
func (v LevelVar) Level() slog.Level {
	data := []byte(v)

	var level slog.Level
	// unmarshal the level
	_ = level.UnmarshalText(data)
	// done!
	return level
}

// MarshalText implements [encoding.TextMarshaler] by calling [Level.MarshalText].
func (v *LevelVar) MarshalText() ([]byte, error) {
	return v.Level().MarshalText()
}

// UnmarshalText implements [encoding.TextUnmarshaler] by calling [Level.UnmarshalText].
func (v *LevelVar) UnmarshalText(data []byte) error {
	var level slog.Level

	if err := level.UnmarshalText(data); err != nil {
		return err
	}

	*v = LevelVar(level.String())
	return nil
}
