package webhookhttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
)

// Header names follow the Standard Webhooks spelling (lowercase). They are assigned to the
// header map directly, because Header.Set would canonicalize them to Webhook-Id.
const (
	headerWebhookID  = "webhook-id"
	headerTimestamp  = "webhook-timestamp"
	headerSignature  = "webhook-signature"
	contentTypeJSON  = "application/json"
	responseReadSize = entity.MaxResponseBodyBytes + 1 // one extra byte tells "truncated" from "exactly full"
)

// UserAgent is the User-Agent value that identifies Hookrelay to receivers.
type UserAgent string

// SenderOptions configure a Sender. Guarded serves ordinary destinations, Allowlisted the URLs
// that IsAllowlisted accepts.
type SenderOptions struct {
	Guarded       *http.Client
	Allowlisted   *http.Client
	IsAllowlisted func(rawURL string) bool
	UserAgent     UserAgent
	Timeout       time.Duration
}

// Sender implements repository.Sender over net/http.
type Sender struct{ opts SenderOptions }

// NewSender builds a Sender.
func NewSender(o SenderOptions) *Sender { return &Sender{opts: o} }

// Send posts the signed request. One context bounds dial, TLS, headers and the 4 KB body read.
// Every failure to get a response is an Outcome; the only error is ErrSendAborted, returned
// when ctx itself ended.
func (s *Sender) Send(ctx context.Context, req entity.SignedRequest) (entity.Outcome, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	httpReq, err := s.newRequest(attemptCtx, req)
	if err != nil {
		return entity.FailedOutcome(entity.ErrorConnection), nil // an unusable URL is a connection problem, not a crash
	}
	resp, err := s.clientFor(req.URL).Do(httpReq)
	if err != nil {
		return classify(ctx, err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing to recover: the connection is dropped, not reused
	// A read error after the status line is ignored on purpose: the status decides the outcome.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, responseReadSize))
	return entity.HTTPOutcome(resp.StatusCode, raw), nil
}

func (s *Sender) newRequest(ctx context.Context, req entity.SignedRequest) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	httpReq.Header[headerWebhookID] = []string{req.WebhookID}
	httpReq.Header[headerTimestamp] = []string{strconv.FormatInt(req.Timestamp, 10)}
	httpReq.Header[headerSignature] = []string{req.Signature}
	httpReq.Header.Set("Content-Type", contentTypeJSON)
	httpReq.Header.Set("User-Agent", string(s.opts.UserAgent))
	return httpReq, nil
}

func (s *Sender) clientFor(rawURL string) *http.Client {
	if s.opts.IsAllowlisted(rawURL) {
		return s.opts.Allowlisted
	}
	return s.opts.Guarded
}
