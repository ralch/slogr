package stack

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"go.opencensus.io/trace"
	"golang.org/x/exp/slog"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	NameKey      = "name"
	LabelKey     = "labels"
	RequestKey   = "request"
	ResponseKey  = "response"
	OperationKey = "operation"
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
		name      = h.name(ctx, r)
		trace     = h.trace(ctx, r)
		labels    = h.label(ctx, r)
		severity  = h.severity(ctx, r)
		location  = h.location(ctx, r)
		request   = h.request(ctx, r)
		payload   = h.payload(ctx, r)
		operation = h.operation(ctx, r)
		timestamp = timestamppb.New(r.Time)
	)

	entry := &loggingpb.LogEntry{
		LogName:        name,
		Severity:       severity,
		Timestamp:      timestamp,
		Payload:        payload,
		Labels:         labels,
		HttpRequest:    request,
		Operation:      operation,
		SourceLocation: location,
		Trace:          trace.Name,
		TraceSampled:   trace.SpanContext.IsSampled(),
		SpanId:         trace.SpanContext.SpanID.String(),
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

func (h *handler) name(_ context.Context, r slog.Record) string {
	var name string

	r.Attrs(func(attr slog.Attr) {
		if attr.Key == NameKey {
			name = h.project + "/" + url.PathEscape(attr.Value.String())
		}
	})

	return name
}

func (h *handler) payload(_ context.Context, r slog.Record) *loggingpb.LogEntry_JsonPayload {
	r.Attrs(func(attr slog.Attr) {
		// TODO:
	})

	return nil
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

func (h *handler) request(_ context.Context, r slog.Record) *ltype.HttpRequest {
	var request *ltype.HttpRequest

	r.Attrs(func(attr slog.Attr) {
		if attr.Key == RequestKey {
			value, _ := attr.Value.Any().(*ltype.HttpRequest)
			proto.Merge(request, value)
		}

		if attr.Key == ResponseKey {
			value, _ := attr.Value.Any().(*ltype.HttpRequest)
			proto.Merge(request, value)
		}
	})

	return request
}

func (h *handler) operation(_ context.Context, r slog.Record) *loggingpb.LogEntryOperation {
	var operation *loggingpb.LogEntryOperation

	r.Attrs(func(attr slog.Attr) {
		if attr.Key == OperationKey {
			operation, _ = attr.Value.Any().(*loggingpb.LogEntryOperation)
		}
	})

	return operation
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

// Name returns an Attr for a log name.
// The caller must not subsequently mutate the
// argument slice.
//
// Use Label to collect several Attrs under a name
// key on a log line.
func Name(value string) slog.Attr {
	value = url.PathEscape(value)

	return slog.Attr{
		Key:   NameKey,
		Value: slog.StringValue(value),
	}
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

// Request returns an Attr for a http.Request.
// The caller must not subsequently mutate the
// argument slice.
//
// Use Request to collect several Attrs under a HttpRequest
// key on a log line.
func Request(r *http.Request) slog.Attr {
	if r.URL == nil {
		r.URL = &url.URL{}
	}

	remoteIP := func() string {
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			return ip
		}

		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}

		return ip
	}

	serverIP := func() string {
		if ip, err := net.LookupHost(r.Host); err == nil {
			return ip[0]
		}

		return ""
	}

	value := &ltype.HttpRequest{
		Protocol:      r.Proto,
		RequestMethod: r.Method,
		RequestUrl:    r.URL.String(),
		RequestSize:   r.ContentLength,
		Referer:       r.Referer(),
		UserAgent:     r.UserAgent(),
		RemoteIp:      remoteIP(),
		ServerIp:      serverIP(),
	}

	return slog.Attr{
		Key:   RequestKey,
		Value: slog.AnyValue(value),
	}
}

// Respone returns an Attr for a http.Respone.
// The caller must not subsequently mutate the
// argument slice.
//
// Use Response to collect several Attrs under a HttpRequest
// key on a log line.
func Response(r *http.Response) slog.Attr {
	value := &ltype.HttpRequest{
		ResponseSize: r.ContentLength,
		Status:       int32(r.StatusCode),
	}

	return slog.Attr{
		Key:   ResponseKey,
		Value: slog.AnyValue(value),
	}
}

// ResponseWriter returns an Attr for a http.ResponseWriter.
// The caller must not subsequently mutate the
// argument slice.
//
// Use Response to collect several Attrs under a HttpRequest
// key on a log line.
func ResponseWriter(r http.ResponseWriter) slog.Attr {
	type ResponseWriter interface {
		GetStatusCode() int32
		GetContentLength() int64
	}

	value := &ltype.HttpRequest{}

	if rw, ok := r.(ResponseWriter); ok {
		value = &ltype.HttpRequest{
			Status:       rw.GetStatusCode(),
			ResponseSize: rw.GetContentLength(),
		}
	}

	return slog.Attr{
		Key:   ResponseKey,
		Value: slog.AnyValue(value),
	}
}

// OperationStart is a function for logging `Operation`. It should be called
// for the first operation log.
func OperationStart(id, producer string) slog.Attr {
	value := &loggingpb.LogEntryOperation{
		Id:       id,
		Producer: producer,
		First:    true,
		Last:     false,
	}

	return slog.Attr{
		Key:   OperationKey,
		Value: slog.AnyValue(value),
	}
}

// OperationContinue is a function for logging `Operation`. It should be called
// for any non-start/end operation log.
func OperationContinue(id, producer string) slog.Attr {
	value := &loggingpb.LogEntryOperation{
		Id:       id,
		Producer: producer,
		First:    false,
		Last:     false,
	}

	return slog.Attr{
		Key:   OperationKey,
		Value: slog.AnyValue(value),
	}
}

// OperationEnd is a function for logging `Operation`. It should be called
// for the last operation log.
func OperationEnd(id, producer string) slog.Attr {
	value := &loggingpb.LogEntryOperation{
		Id:       id,
		Producer: producer,
		First:    false,
		Last:     true,
	}

	return slog.Attr{
		Key:   OperationKey,
		Value: slog.AnyValue(value),
	}
}

// Error returns an error attribute
func Error(err error) slog.Attr {
	return slog.Attr{
		Key:   OperationKey,
		Value: slog.StringValue(err.Error()),
	}
}
