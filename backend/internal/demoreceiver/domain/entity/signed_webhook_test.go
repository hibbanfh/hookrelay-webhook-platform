package entity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
)

func headers(mutate func(*entity.WebhookHeaders)) entity.WebhookHeaders {
	h := entity.WebhookHeaders{ID: "msg_1", Timestamp: "1700000000", Signature: "v1,abc"}
	mutate(&h)
	return h
}

func TestParseSignedWebhook(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*entity.WebhookHeaders)
		wantErr error
	}{
		{name: "valid", mutate: func(*entity.WebhookHeaders) {}},
		{name: "id of 255 characters", mutate: func(h *entity.WebhookHeaders) { h.ID = strings.Repeat("a", 255) }},
		{name: "id with printable punctuation", mutate: func(h *entity.WebhookHeaders) { h.ID = "!~_-.:/" }},
		{name: "timestamp of 12 digits", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "999999999999" }},
		{name: "missing id", mutate: func(h *entity.WebhookHeaders) { h.ID = "" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "id of 256 characters", mutate: func(h *entity.WebhookHeaders) { h.ID = strings.Repeat("a", 256) }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "id with a space", mutate: func(h *entity.WebhookHeaders) { h.ID = "msg 1" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "id with a control character", mutate: func(h *entity.WebhookHeaders) { h.ID = "msg\t1" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "id with a non ASCII character", mutate: func(h *entity.WebhookHeaders) { h.ID = "msg_é" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "missing timestamp", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "negative timestamp", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "-1" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "signed timestamp", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "+1" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "exponent timestamp", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "1e9" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "fractional timestamp", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "1700000000.5" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "timestamp of 13 digits", mutate: func(h *entity.WebhookHeaders) { h.Timestamp = "1000000000000" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "missing signature", mutate: func(h *entity.WebhookHeaders) { h.Signature = "" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "only another version", mutate: func(h *entity.WebhookHeaders) { h.Signature = "v1a,xyz" }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "version without a value", mutate: func(h *entity.WebhookHeaders) { h.Signature = "v1," }, wantErr: domainerrors.ErrInvalidSignature},
		{name: "eleven entries", mutate: func(h *entity.WebhookHeaders) { h.Signature = strings.Repeat("v1,a ", 11) }, wantErr: domainerrors.ErrInvalidSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.ParseSignedWebhook(headers(tt.mutate), []byte("body"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSignedWebhookKeepsWhatWasSigned(t *testing.T) {
	h := headers(func(h *entity.WebhookHeaders) { h.Signature = "v1a,x v1,abc" })
	body := []byte(" {\"b\":1,  \"a\":2}\n")

	got, err := entity.ParseSignedWebhook(h, body)

	if err != nil || got.ID != "msg_1" || got.Timestamp != 1700000000 || string(got.Body) != string(body) ||
		len(got.Signatures) != 1 || got.Signatures[0] != "v1,abc" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSignedWebhookIsFresh(t *testing.T) {
	now := time.Unix(1700000000, 0)
	tests := []struct {
		name   string
		offset int64
		want   bool
	}{
		{name: "301 seconds old", offset: -301},
		{name: "300 seconds old", offset: -300, want: true},
		{name: "now", offset: 0, want: true},
		{name: "300 seconds ahead", offset: 300, want: true},
		{name: "301 seconds ahead", offset: 301},
		{name: "far in the future", offset: 999_999_999_999 - 1700000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wh := entity.SignedWebhook{Timestamp: now.Unix() + tt.offset}
			if got := wh.IsFresh(now); got != tt.want {
				t.Fatalf("IsFresh with offset %d = %v, want %v", tt.offset, got, tt.want)
			}
		})
	}
}

func TestSignedWebhookIsFreshIgnoresSubSecondTime(t *testing.T) {
	now := time.Unix(1700000300, 999_999_999)
	wh := entity.SignedWebhook{Timestamp: 1700000000}

	if !wh.IsFresh(now) {
		t.Fatal("a request exactly 300 whole seconds old must be fresh")
	}
}
