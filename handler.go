package stack

import (
	"context"
	"encoding/json"
	"io"
	"runtime"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"go.opencensus.io/trace"
	"golang.org/x/exp/slog"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	LabelKey = "logging.googleapis.com/label"
)

// Options for a slog.Handler that writes tinted logs. A zero Options consists
// entirely of default values.
type Options struct {
	// ProjectId is Google Cloud Project ID
	// If you want to use trace_id, you should set this or set GOOGLE_CLOUD_PROJECT environment.
	// Cloud Shell and App Engine set this environment variable to the project ID, so use it if present.
	ProjectID string

	// Minimum level to log (Default: slog.InfoLevel)
	Level slog.Level
}

// NewHandler creates a [slog.Handler] that writes tinted logs to w with the
// given options.
func (opts Options) NewHandler(w io.Writer) slog.Handler {
	h := &handler{
		w:       w,
		level:   opts.Level,
		project: "projects/" + opts.ProjectID,
	}

	return h
}

// NewHandler creates a [slog.Handler] that writes tinted logs to w, using the default
// options.
func NewHandler(w io.Writer) slog.Handler {
	return (Options{}).NewHandler(w)
}

// handler implements a [slog.handler].
type handler struct {
	w       io.Writer
	level   slog.Level
	project string
}

// Enabled implements slog.Handler
func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle implements slog.Handler
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	var (
		trace    = h.trace(ctx, r)
		labels   = h.label(ctx, r)
		severity = h.severity(ctx, r)
		location = h.location(ctx, r)
	)

	entry := &loggingpb.LogEntry{
		LogName: "",
		// Resource:         &monitoredres.MonitoredResource{},
		Payload: &loggingpb.LogEntry_TextPayload{
			TextPayload: r.Message,
		},
		Timestamp: timestamppb.New(r.Time),
		Severity: severity,
		// InsertId:         "",
		// HttpRequest:      &ltype.HttpRequest{},
		Labels: labels,
		// Operation:        &loggingpb.LogEntryOperation{},
		SourceLocation: location,
		Trace:          trace.Name,
		TraceSampled:   trace.SpanContext.IsSampled(),
		SpanId:         trace.SpanContext.SpanID.String(),
		// Split: &loggingpb.LogSplit{},
	}

	encoder := json.NewEncoder(h.w)

	return encoder.Encode(entry)
}

// WithAttrs implements slog.Handler
func (*handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	panic("unimplemented")
}

// WithGroup implements slog.Handler
func (*handler) WithGroup(name string) slog.Handler {
	panic("unimplemented")
}

func (h *handler) severity(_ context.Context, r slog.Record) ltype.LogSeverity {
	switch r.Level {
	case slog.LevelDebug:
		return ltype.LogSeverity_DEBUG
	case slog.LevelInfo:
		return ltype.LogSeverity_INFO
	case slog.LevelWarn:
		return ltype.LogSeverity_WARNING
	case slog.LevelError:
		return ltype.LogSeverity_ERROR
	default:
		return ltype.LogSeverity_DEFAULT
	}
}

func (h *handler) location(_ context.Context, r slog.Record) *loggingpb.LogEntrySourceLocation {
	frames := runtime.CallersFrames([]uintptr{r.PC})
	frame, _ := frames.Next()

	return &loggingpb.LogEntrySourceLocation{
		File:     frame.File,
		Line:     int64(frame.Line),
		Function: frame.Function,
	}
}

func (h *handler) trace(ctx context.Context, _ slog.Record) *trace.SpanData {
	data := &trace.SpanData{}

	if span := trace.FromContext(ctx); span != nil {
		data.SpanContext = span.SpanContext()
		data.Name = h.project + "/" + data.SpanContext.TraceID.String()
	}

	return data
}

func (h *handler) label(_ context.Context, r slog.Record) map[string]string {
	kv := make(map[string]string)

	r.Attrs(func(attr slog.Attr) {
		if attr.Key == LabelKey {
			for _, label := range attr.Value.Group() {
				kv[label.Key] = label.Value.String()
			}
		}
	})

	return kv
}

// Label returns an Attr for a Group Label.
// The caller must not subsequently mutate the
// argument slice.
//
// Use Label to collect several Attrs under a labels
// key on a log line.
func Label(attr ...slog.Attr) slog.Attr {
	return slog.Attr{
		Key:   LabelKey,
		Value: slog.GroupValue(attr...),
	}
}
