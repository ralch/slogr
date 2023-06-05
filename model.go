package slogr

import (
	"context"
	"encoding/json"
	"reflect"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
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

var _ json.Marshaler = &Entry{}

// An individual entry in a log.
type Entry loggingpb.LogEntry

// GetPayload returns the underlying payload
func (x *Entry) GetPayload() interface{} {
	switch payload := x.Payload.(type) {

	case *loggingpb.LogEntry_TextPayload:
		return payload.TextPayload
	case *loggingpb.LogEntry_JsonPayload:
		return payload.JsonPayload
	case *loggingpb.LogEntry_ProtoPayload:
		return payload.ProtoPayload
	}

	return nil
}

// GetJsonPayload returns the log entry payload, represented as a structure that is
// expressed as a JSON object.
func (x *Entry) GetJsonPayload() *structpb.Struct {
	if payload, ok := x.Payload.(*loggingpb.LogEntry_JsonPayload); ok {
		return payload.JsonPayload
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
func (x *Entry) MarshalJSON() ([]byte, error) {
	attributes := make(map[string]interface{})

	set := func(k string, v interface{}) error {
		if value := reflect.ValueOf(v); !value.IsValid() || value.IsZero() {
			return nil
		}

		if msg, ok := v.(proto.Message); ok {
			if msg == nil {
				return nil
			}

			// marshal the message in JSON format
			data, err := protojson.Marshal(msg)
			if err != nil {
				return err
			}

			entity := make(map[string]interface{})
			// convert the data to a map[string]interface{}
			if err = json.Unmarshal(data, &entity); err != nil {
				return err
			}

			v = entity
		}

		attributes[k] = v
		// done
		return nil
	}

	set("severity", x.Severity.String())
	set("httpRequest", x.HttpRequest)
	set("timestamp", x.Timestamp.AsTime())
	set("message", x.GetPayload())
	set("logging.googleapis.com/insertId", x.InsertId)
	set("logging.googleapis.com/labels", x.Labels)
	set("logging.googleapis.com/operation", x.Operation)
	set("logging.googleapis.com/sourceLocation", x.SourceLocation)
	set("logging.googleapis.com/spanId", x.SpanId)
	set("logging.googleapis.com/trace", x.Trace)
	set("logging.googleapis.com/trace_sampled", x.TraceSampled)

	return json.Marshal(attributes)
}
