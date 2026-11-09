package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// RequestIDHeader carries the request ID on requests (optional) and responses.
const RequestIDHeader = "X-Request-Id"

// healthPath is probed every few seconds by orchestrators, so its access line is debug-level.
const healthPath = "/healthz"

const maxRequestIDLength = 64

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Clock provides the current time (injected so tests are deterministic).
type Clock interface{ Now() time.Time }

type requestIDKey struct{}

// RequestID returns the request ID stored by the RequestID middleware, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Chain applies mws so that the first one is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// WithRequestID exposes a request ID in the response header and stores it in the context.
// The contract (api/openapi.yaml) echoes an inbound X-Request-Id when it matches
// ^[A-Za-z0-9._-]{1,64}$; anything else is replaced by a generated ID. The strict charset
// keeps newlines and quotes out of the logs.
func WithRequestID(newID func() string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !isValidRequestID(id) {
				id = newID()
			}
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		})
	}
}

func isValidRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}
	return !strings.ContainsFunc(id, func(c rune) bool { return !isRequestIDChar(c) })
}

func isRequestIDChar(c rune) bool {
	isAlnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	return isAlnum || c == '.' || c == '_' || c == '-'
}

// AccessLog emits one structured log line per request (debug level for /healthz).
func AccessLog(log *slog.Logger, clock Clock) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := clock.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.LogAttrs(r.Context(), accessLogLevel(r.URL.Path), "request",
				slog.String("request_id", RequestID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", clock.Now().Sub(start).Milliseconds()),
			)
		})
	}
}

func accessLogLevel(path string) slog.Level {
	if path == healthPath {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// Recover turns a panic in a handler into a logged 500 response.
func Recover(log *slog.Logger) Middleware {
	resp := NewResponder(log)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			defer recoverPanic(log, resp, sw, r)
			next.ServeHTTP(sw, r)
		})
	}
}

// recoverPanic must be the deferred function itself so that recover() works.
// http.ErrAbortHandler is the sanctioned way to abort a response, so it is passed on to
// net/http. Once headers are sent a 500 can no longer be written, so only the log remains.
func recoverPanic(log *slog.Logger, resp Responder, sw *statusWriter, r *http.Request) {
	p := recover()
	if p == nil {
		return
	}
	err, isErr := p.(error)
	if isErr && errors.Is(err, http.ErrAbortHandler) {
		panic(p)
	}
	log.ErrorContext(r.Context(), "panic recovered",
		slog.Any("panic", p),
		slog.String("stack", string(debug.Stack())),
		slog.String("request_id", RequestID(r.Context())))
	if sw.wroteHeader {
		return
	}
	resp.Error(sw, r, errInternal)
}

// BodyLimit caps the request body; reads past the limit fail with *http.MaxBytesError.
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout cancels the request context after d. Handlers and repositories honour it.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// statusWriter remembers the first status code (access log) and whether the response
// has started (Recover).
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status, s.wroteHeader = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
