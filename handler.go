package slogr

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	NameKey      = "name"
	ErrorKey     = "error"
	LabelKey     = "labels"
	RequestKey   = "request"
	ResponseKey  = "response"
	OperationKey = "operation"
)

// HandlerOptions for a slog.Handler that writes tinted logs. A zero HandlerOptions consists
// entirely of default values.
type HandlerOptions struct {
	// ProjectID is Google Cloud Project ID
	// If you want to use trace_id, you should set this or set GOOGLE_CLOUD_PROJECT environment.
	// Cloud Shell and App Engine set this environment variable to the project ID, so use it if present.
	ProjectID string

	// When AddIndent is true, the handler adds an ident to the JSON output.
	AddIndent bool

	// When AddSource is true, the handler adds a ("source", "file:line")
	// attribute to the output indicating the source code position of the log
	// statement. AddSource is false by default to skip the cost of computing
	// this information.
	AddSource bool

	// Level reports the minimum record level that will be logged.
	// The handler discards records with lower levels.
	// If Level is nil, the handler assumes LevelInfo.
	// The handler calls Level.Level for each record processed;
	// to adjust the minimum level dynamically, use a LevelVar.
	Level slog.Leveler
}

// Handler implements a [slog.Handler].
type Handler struct {
	leveler slog.Leveler
	writer  io.Writer
	project string
	source  bool
	indent  bool
	attr    []slog.Attr
}

// NewHandler creates a [slog.Handler] that writes tinted logs to w, using the default
// options.
func NewHandler(w io.Writer, opts *HandlerOptions) slog.Handler {
	h := &Handler{
		writer:  w,
		leveler: opts.Level,
		source:  opts.AddSource,
		indent:  opts.AddIndent,
		project: opts.ProjectID,
	}

	return h
}

// Enabled implements slog.Handler
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.leveler.Level()
}

// Handle implements slog.Handler
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	r = h.record(r)

	var (
		name      = h.name(ctx, r)
		labels    = h.label(ctx, r)
		severity  = h.severity(ctx, r)
		location  = h.location(ctx, r)
		request   = h.request(ctx, r)
		payload   = h.payload(ctx, r)
		operation = h.operation(ctx, r)
		timestamp = timestamppb.New(r.Time)
	)

	entry := &Entry{
		LogName:        name,
		Severity:       severity,
		Timestamp:      timestamp,
		Labels:         labels,
		HttpRequest:    request,
		Operation:      operation,
		SourceLocation: location,
	}

	switch value := payload.(type) {
	case *loggingpb.LogEntry_JsonPayload:
		entry.Payload = value
	case *loggingpb.LogEntry_TextPayload:
		entry.Payload = value
	}

	if span := h.trace(ctx, r); span != nil {
		entry.Trace = h.path("traces", span.TraceID().String())
		entry.TraceSampled = span.IsSampled()
		entry.SpanId = span.SpanID().String()
	}

	encoder := json.NewEncoder(h.writer)
	// enables the pretty format
	if h.indent {
		encoder.SetIndent("", "  ")
	}

	return encoder.Encode(entry)
}

// WithAttrs implements slog.Handler
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := h.clone()
	c.attr = append(c.attr, attrs...)
	return c
}

// WithGroup implements slog.Handler
func (h *Handler) WithGroup(name string) slog.Handler {
	c := h.clone()
	return c
}

func (h *Handler) severity(_ context.Context, r slog.Record) ltype.LogSeverity {
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

func (h *Handler) name(_ context.Context, r slog.Record) string {
	var name string

	if h.project != "" {
		r.Attrs(func(attr slog.Attr) bool {
			if attr.Key == NameKey {
				name = h.path("logs", url.PathEscape(attr.Value.String()))
				return false
			}

			return true
		})
	}

	return name
}

func (h *Handler) payload(_ context.Context, r slog.Record) interface{} {
	props := make(map[string]interface{})

	r.Attrs(func(attr slog.Attr) bool {
		switch attr.Key {
		case NameKey:
			return true
		case LabelKey:
			return true
		case RequestKey:
			return true
		case ResponseKey:
			return true
		case OperationKey:
			return true
		default:
			props[attr.Key] = h.value(attr.Value)
			return true
		}
	})

	if count := len(props); count == 0 {
		return &loggingpb.LogEntry_TextPayload{
			TextPayload: r.Message,
		}
	}

	props["logging.googleapis.com/message"] = r.Message
	// construct the payload
	value, err := structpb.NewStruct(props)
	if err != nil {
		panic(err)
	}

	return &loggingpb.LogEntry_JsonPayload{
		JsonPayload: value,
	}
}

func (h *Handler) location(_ context.Context, r slog.Record) *loggingpb.LogEntrySourceLocation {
	if h.source {
		frames := runtime.CallersFrames([]uintptr{r.PC})
		frame, _ := frames.Next()

		return &loggingpb.LogEntrySourceLocation{
			File:     frame.File,
			Line:     int64(frame.Line),
			Function: frame.Function,
		}
	}

	return nil
}

func (h *Handler) request(_ context.Context, r slog.Record) *ltype.HttpRequest {
	var (
		count   = 0
		request = &ltype.HttpRequest{}
	)

	r.Attrs(func(attr slog.Attr) bool {
		if attr.Key == RequestKey {
			request, _ = attr.Value.Any().(*ltype.HttpRequest)
			// done!
			count++
			return false
		}

		return true
	})

	r.Attrs(func(attr slog.Attr) bool {
		if attr.Key == ResponseKey {
			response, _ := attr.Value.Any().(*ltype.HttpRequest)
			// merge the request and response
			request.Status = response.Status
			request.ResponseSize = response.ResponseSize
			// done!
			count++
			return false
		}

		return true
	})

	if count == 0 {
		request = nil
	}

	return request
}

func (h *Handler) operation(_ context.Context, r slog.Record) *loggingpb.LogEntryOperation {
	var operation *loggingpb.LogEntryOperation

	r.Attrs(func(attr slog.Attr) bool {
		if attr.Key == OperationKey {
			operation, _ = attr.Value.Any().(*loggingpb.LogEntryOperation)
			return false
		}

		return true
	})

	return operation
}

func (h *Handler) trace(ctx context.Context, _ slog.Record) *trace.SpanContext {
	if h.project != "" {
		if span := trace.SpanFromContext(ctx); span != nil {
			if sctx := span.SpanContext(); sctx.IsValid() {
				return &sctx
			}
		}
	}

	return nil
}

func (h *Handler) label(_ context.Context, r slog.Record) map[string]string {
	kv := make(map[string]string)

	r.Attrs(func(attr slog.Attr) bool {
		if attr.Key == LabelKey {
			for _, item := range attr.Value.Group() {
				for _, label := range h.flatten(item) {
					kv[label.Key] = label.Value.String()
				}
			}

			return false
		}

		return true
	})

	return kv
}

func (h *Handler) path(key ...string) string {
	path := []string{}
	path = append(path, "projects")
	path = append(path, h.project)
	path = append(path, key...)

	return filepath.Join(path...)
}

func (h *Handler) value(v slog.Value) interface{} {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration()
	case slog.KindTime:
		return v.Time()
	case slog.KindAny:
		return v.Any()
	case slog.KindLogValuer:
		return h.value(v.LogValuer().LogValue())
	case slog.KindGroup:
		kv := make(map[string]interface{})

		for _, attr := range v.Group() {
			kv[attr.Key] = h.value(attr.Value)
		}

		return kv
	default:
		return nil
	}
}

func (h *Handler) flatten(attr slog.Attr) []slog.Attr {
	var collection []slog.Attr

	switch attr.Value.Kind() {
	case slog.KindGroup:
		for _, item := range attr.Value.Group() {
			elem := slog.Attr{
				Key:   attr.Key + "." + item.Key,
				Value: item.Value,
			}
			collection = append(collection, h.flatten(elem)...)
		}
	default:
		collection = append(collection, attr)
	}

	return collection
}

func (h *Handler) clone() *Handler {
	return &Handler{
		leveler: h.leveler,
		writer:  h.writer,
		project: h.project,
		source:  h.source,
		indent:  h.indent,
		attr:    h.attr,
	}
}

func (h *Handler) record(r slog.Record) slog.Record {
	r = r.Clone()
	r.AddAttrs(h.attr...)
	return r
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
func Label(attr ...any) slog.Attr {
	return slog.Group(LabelKey, attr...)
}

// Request returns an Attr for a http.Request.
// The caller must not subsequently mutate the
// argument slice.
//
// Use Request to collect several Attrs under a HttpRequest
// key on a log line.
func Request(r *http.Request) slog.Attr {
	r = r.Clone(context.Background())

	scheme := func() string {
		if value := r.Header.Get("X-Forwarded-Proto"); value != "" {
			return value
		}

		if r.TLS == nil {
			return "http"
		} else {
			return "https"
		}
	}

	host := func() string {
		if value := r.Header.Get("X-Forwarded-Host"); value != "" {
			return value
		}

		return r.Host
	}

	remote := func() string {
		if value := r.Header.Get("X-Forwarded-For"); value != "" {
			return value
		}

		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}

		return ip
	}

	r.Host = host()
	r.RemoteAddr = remote()
	r.URL.Scheme = scheme()
	r.URL.Host = host()

	value := &ltype.HttpRequest{
		Protocol:      r.Proto,
		RequestMethod: r.Method,
		RequestSize:   r.ContentLength,
		RequestUrl:    r.URL.String(),
		RemoteIp:      r.RemoteAddr,
		Referer:       r.Referer(),
		UserAgent:     r.UserAgent(),
	}

	// we don't know the latency at this stage.
	value.Latency = durationpb.New(0)
	// set the remote ip
	if addresses, err := net.LookupHost(r.Host); err == nil {
		value.RemoteIp = addresses[0]
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
		Key:   ErrorKey,
		Value: slog.StringValue(err.Error()),
	}
}
