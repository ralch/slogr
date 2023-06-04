package slogr

import (
	"encoding/json"
	"reflect"

	"cloud.google.com/go/logging/apiv2/loggingpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var _ json.Marshaler = &Entry{}

// An individual entry in a log.
type Entry loggingpb.LogEntry

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

	set("severity", x.Severity)
	set("httpRequest", x.HttpRequest)
	set("timestamp", x.Timestamp.AsTime())
	set("logging.googleapis.com/insertId", x.InsertId)
	set("logging.googleapis.com/labels", x.Labels)
	set("logging.googleapis.com/operation", x.Operation)
	set("logging.googleapis.com/sourceLocation", x.SourceLocation)
	set("logging.googleapis.com/spanId", x.SpanId)
	set("logging.googleapis.com/trace", x.Trace)
	set("logging.googleapis.com/trace_sampled", x.TraceSampled)

	return json.Marshal(attributes)
}
