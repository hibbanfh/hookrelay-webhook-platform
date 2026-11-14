package entity_test

import (
	"encoding/base64"
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
)

// The published Standard Webhooks test vector (ADR 0007).
const (
	vectorSecret    = "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw" // the part after "whsec_"
	vectorID        = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	vectorTimestamp = int64(1614265330)
	vectorBody      = `{"test": 2432232314}`
	vectorSignature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func vectorKey(t *testing.T) []byte {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(vectorSecret)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSign(t *testing.T) {
	tests := []struct {
		name string
		id   string
		ts   int64
		body string
		want string
	}{
		{name: "published vector", id: vectorID, ts: vectorTimestamp, body: vectorBody, want: vectorSignature},
		{name: "one body byte changed", id: vectorID, ts: vectorTimestamp, body: `{"test": 2432232315}`},
		{name: "other timestamp", id: vectorID, ts: vectorTimestamp + 1, body: vectorBody},
		{name: "other id", id: "msg_other", ts: vectorTimestamp, body: vectorBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entity.Sign(vectorKey(t), tt.id, tt.ts, []byte(tt.body))

			if tt.want != "" && got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
			if tt.want == "" && got == vectorSignature {
				t.Fatalf("a changed input must change the signature, got %s", got)
			}
		})
	}
}

func TestNewSignedRequestSignsTheStoredPayloadUntouched(t *testing.T) {
	payload := []byte(`{"b":1,  "a":[1,2], "a":3}`)
	job := entity.Job{
		Delivery: entity.Delivery{MessageID: vectorID},
		Target:   entity.Target{URL: "https://example.com/hook", Key: entity.NewSigningKey(vectorKey(t))},
		Payload:  payload,
	}

	req := entity.NewSignedRequest(job, vectorTimestamp)

	wantSig := entity.Sign(vectorKey(t), vectorID, vectorTimestamp, payload)
	if req.WebhookID != vectorID || req.Timestamp != vectorTimestamp || req.Signature != wantSig || req.URL != job.Target.URL {
		t.Fatalf("got %+v", req)
	}
	if &req.Body[0] != &payload[0] {
		t.Fatal("the body must be the payload slice itself")
	}
}
