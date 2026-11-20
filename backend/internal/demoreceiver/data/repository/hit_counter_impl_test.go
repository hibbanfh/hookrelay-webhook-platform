package repositoryimpl_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/demoreceiver/data/model"
	repositoryimpl "hookrelay/internal/demoreceiver/data/repository"
	"hookrelay/internal/demoreceiver/domain/entity"
	"hookrelay/internal/demoreceiver/domain/repository"
)

var errBoom = errors.New("boom")

type fakeSource struct {
	n        int
	err      error
	sweepErr error
	got      model.HitIncrement
	swept    []model.HitIncrement
}

func (f *fakeSource) Sweep(_ context.Context, in model.HitIncrement) error {
	f.swept = append(f.swept, in)
	return f.sweepErr
}

func newCounter(src *fakeSource, logs *bytes.Buffer) repository.HitCounter {
	return repositoryimpl.NewHitCounter(src, slog.New(slog.NewTextHandler(logs, nil)))
}

func (f *fakeSource) Increment(_ context.Context, in model.HitIncrement) (int, error) {
	f.got = in
	return f.n, f.err
}

func TestHitCounterMapsTheRequestToTheModelInUTC(t *testing.T) {
	zone := time.FixedZone("x", 7*3600)
	at := time.Date(2026, 11, 20, 19, 0, 0, 0, zone)
	expired := at.Add(-entity.HitRetention)
	src := &fakeSource{n: 3}

	n, err := newCounter(src, &bytes.Buffer{}).Increment(context.Background(), repository.HitIncrement{
		Key: entity.HitKey{EndpointID: "ep_1", WebhookID: "msg_1"}, At: at, ExpiredBefore: expired,
	})

	want := model.HitIncrement{EndpointID: "ep_1", WebhookID: "msg_1", At: at.UTC(), ExpiredBefore: expired.UTC()}
	if err != nil || n != 3 || src.got != want || src.got.At.Location() != time.UTC || len(src.swept) != 1 || src.swept[0] != want {
		t.Fatalf("got %d, %v, model %+v; want 3 and %+v", n, err, src.got, want)
	}
}

func TestHitCounterWrapsStorageErrorsAndDoesNotSweepAfterAFailedCount(t *testing.T) {
	src := &fakeSource{err: errBoom}

	_, err := newCounter(src, &bytes.Buffer{}).Increment(context.Background(), repository.HitIncrement{
		Key: entity.HitKey{EndpointID: "ep_1", WebhookID: "msg_1"},
	})

	if !errors.Is(err, errBoom) || len(src.swept) != 0 {
		t.Fatalf("got %v and %d sweeps, want a wrapped errBoom and no sweep", err, len(src.swept))
	}
}

// The count is what the caller needs. A failed sweep is housekeeping: it is logged and the count stands.
func TestHitCounterKeepsTheCountWhenTheSweepFails(t *testing.T) {
	src := &fakeSource{n: 4, sweepErr: errBoom}
	var logs bytes.Buffer

	n, err := newCounter(src, &logs).Increment(context.Background(), repository.HitIncrement{
		Key: entity.HitKey{EndpointID: "ep_1", WebhookID: "msg_1"},
	})

	if err != nil || n != 4 || strings.Count(logs.String(), "sweep failed") != 1 || !strings.Contains(logs.String(), "ep_1") {
		t.Fatalf("got %d, %v; logs %q; want 4, nil and one sweep warning", n, err, logs.String())
	}
}
