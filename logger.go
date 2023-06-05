package slogr

import (
	"io"

	"golang.org/x/exp/slog"
)

// NewLogger crates a new logger instance.
func NewLogger(w io.Writer, options *HandlerOptions) *slog.Logger {
	// prepare the handler
	handler := NewHandler(w, options)
	// create the logger
	logger := slog.New(handler)
	// done!
	return logger
}
