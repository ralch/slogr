package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bufbuild/connect-go"
	"github.com/go-chi/chi/v5"
	"github.com/gofrs/uuid"
	"github.com/ralch/stack"
	"golang.org/x/exp/slog"
)

func init() {
	// create the options
	// options := slog.HandlerOptions{}
	// create the handler
	// handler := options.NewJSONHandler(os.Stderr)

	// create the options
	options := stack.HandlerOptions{
		ProjectID: "prj-d-platform-952f",
	}
	// create the handler
	handler := options.NewHandler(os.Stderr)

	// create the logger
	logger := slog.New(handler).With(
		stack.Name("run.googleapis.com/user-api"),
		stack.Label(
			slog.String("my_org", "cliche-press"),
			slog.Group("my_app",
				slog.Group("service",
					slog.String("name", "user-api"),
					slog.String("version", "v1.0"),
					slog.String("revision", "ee2c1207"),
				),
			),
		),
	)
	// set the logger
	slog.SetDefault(logger)
}

func main() {
	router := chi.NewRouter()

	router.Use(func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			w = &ResponseWriter{ResponseWriter: w}

			var (
				ctx    = r.Context()
				logger = slog.Default()
			)

			var (
				id        = uuid.Must(uuid.NewV4()).String()
				procedure = "acme.foo.v1.FooService/Bar"
			)

			// start the request
			logger = logger.With(stack.Request(r))
			logger.InfoCtx(ctx, "request received")
			logger.InfoCtx(ctx, "execution started", stack.OperationStart(id, procedure))
			// execute the handler
			next.ServeHTTP(w, r)
			logger.InfoCtx(ctx, "execution finished", stack.OperationEnd(id, procedure))
			// complete the request
			logger = logger.With(stack.ResponseWriter(w))
			logger.InfoCtx(ctx, "request completed")
		}

		return http.HandlerFunc(fn)
	})

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "HELLO")
	})

	if err := http.ListenAndServe("127.0.0.1:9292", router); err != nil {
		panic(err)
	}
}

func Logger(next connect.UnaryFunc) connect.UnaryFunc {
	// prepare the callback
	unaryFn := func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		logger := slog.Default()

		var (
			id   = filepath.Base(request.Spec().Procedure)
			name = filepath.Dir(request.Spec().Procedure)
		)

		logger.InfoCtx(ctx, "execution started", stack.OperationStart(id, name))

		response, err := next(ctx, request)
		switch {
		case err == nil:
			logger.InfoCtx(ctx, "execution finished", stack.OperationEnd(id, name))
		default:
			logger.ErrorCtx(ctx, "execution finished", stack.OperationEnd(id, name), stack.Error(err))
		}

		return response, err
	}

	return unaryFn
}

var _ http.ResponseWriter = &ResponseWriter{}

type ResponseWriter struct {
	StatusCode     int32
	ContentLength  int64
	ResponseWriter http.ResponseWriter
}

// Header implements http.ResponseWriter
func (r *ResponseWriter) Header() http.Header {
	return r.ResponseWriter.Header()
}

// Write implements http.ResponseWriter
func (r *ResponseWriter) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)
	r.ContentLength = r.ContentLength + int64(n)
	return n, err
}

// WriteHeader implements http.ResponseWriter
func (r *ResponseWriter) WriteHeader(code int) {
	r.StatusCode = int32(code)
	r.ResponseWriter.WriteHeader(code)
}

func (r *ResponseWriter) GetStatusCode() int32 {
	return r.StatusCode
}

func (r *ResponseWriter) GetContentLength() int64 {
	return r.ContentLength
}
