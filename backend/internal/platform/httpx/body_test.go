package httpx_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"hookrelay/internal/platform/httpx"
)

const bodyLimit = 262144

func limitedRequest(body []byte, limit int64) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, limit)
	return req
}

func TestReadBodyAtAndAboveTheLimit(t *testing.T) {
	tests := []struct {
		name        string
		size        int
		wantTooBig  bool
		wantReadLen int
	}{
		{name: "exactly the limit is read whole", size: bodyLimit, wantReadLen: bodyLimit},
		{name: "one byte over the limit is refused", size: bodyLimit + 1, wantTooBig: true},
		{name: "empty body", size: 0, wantReadLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpx.ReadBody(limitedRequest(bytes.Repeat([]byte("a"), tt.size), bodyLimit))

			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) != tt.wantTooBig {
				t.Fatalf("too large = %v, want %v (err %v)", errors.As(err, &tooLarge), tt.wantTooBig, err)
			}
			if !tt.wantTooBig && (err != nil || len(got) != tt.wantReadLen) {
				t.Fatalf("got %d bytes, err %v", len(got), err)
			}
		})
	}
}

// An oversized body that is not even JSON must still be a size error: the size check comes
// before any parsing, so the client is told the real problem.
func TestOversizedInvalidJSONIsStillTooLarge(t *testing.T) {
	body := append([]byte("x"), bytes.Repeat([]byte("y"), bodyLimit+10)...)

	_, err := httpx.ReadBody(limitedRequest(body, bodyLimit))

	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("got %v, want *http.MaxBytesError", err)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// A stalled or dropped upload is the client's fault: a 400 that is not logged as a server error.
func TestReadBodyMapsReadFailuresToBadRequest(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "read deadline exceeded", err: os.ErrDeadlineExceeded},
		{name: "connection reset", err: errors.New("connection reset by peer")},
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", failingReader{err: tt.err})
			var logs bytes.Buffer
			rec := httptest.NewRecorder()

			_, err := httpx.ReadBody(req)
			httpx.NewResponder(slog.New(slog.NewTextHandler(&logs, nil))).Error(rec, req, err)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Code != httpx.CodeValidation || !errors.Is(err, tt.err) {
				t.Fatalf("got %v, want a 400 validation error caused by %v", err, tt.err)
			}
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "could not read request body") || logs.Len() != 0 {
				t.Fatalf("status %d body %s logs %q", rec.Code, rec.Body, logs.String())
			}
		})
	}
}
