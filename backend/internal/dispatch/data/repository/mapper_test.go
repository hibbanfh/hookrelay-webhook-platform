package repositoryimpl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/dispatch/data/model"
	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
)

var errBoom = errors.New("boom")

type fakeDeliverySource struct {
	rows       []model.ClaimedDelivery
	claimErr   error
	completeIn model.DeliveryUpdate
	completeEr error
	claimIn    model.ClaimRequest
}

func (f *fakeDeliverySource) Claim(_ context.Context, req model.ClaimRequest) ([]model.ClaimedDelivery, error) {
	f.claimIn = req
	return f.rows, f.claimErr
}

func (f *fakeDeliverySource) CompleteFenced(_ context.Context, u model.DeliveryUpdate) error {
	f.completeIn = u
	return f.completeEr
}

type fakeAttemptSource struct {
	got  model.Attempt
	err  error
	rows []model.Attempt
	list source.AttemptListParams
}

func (f *fakeAttemptSource) ListByDelivery(_ context.Context, p source.AttemptListParams) ([]model.Attempt, error) {
	f.list = p
	return f.rows, f.err
}

func (f *fakeAttemptSource) Insert(_ context.Context, a model.Attempt) error {
	f.got = a
	return f.err
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var now = time.Date(2026, 11, 14, 10, 0, 0, 0, time.FixedZone("UTC+7", 7*3600))

func TestClaimDueSendsTheLeaseAndMapsRows(t *testing.T) {
	src := &fakeDeliverySource{rows: []model.ClaimedDelivery{{ID: "dlv_1", ProjectID: "p", MessageID: "m", EndpointID: "e", Trigger: "manual", AttemptCount: 2}}}
	lease := entity.NewLease("lse_a", now, 30*time.Second)

	got, err := repositoryimpl.NewDeliveryQueue(src, fixedClock{now}).ClaimDue(context.Background(), now, 7, lease)

	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if src.claimIn.Limit != 7 || src.claimIn.LeaseToken != "lse_a" || !src.claimIn.LeaseExpiresAt.Equal(lease.ExpiresAt) {
		t.Fatalf("claim request = %+v", src.claimIn)
	}
	if got[0].Trigger != entity.TriggerManual || got[0].AttemptCount != 2 || got[0].Lease != lease || got[0].Status != entity.StatusPending {
		t.Fatalf("delivery = %+v", got[0])
	}
}

func TestClaimDueReturnsSourceErrors(t *testing.T) {
	_, err := repositoryimpl.NewDeliveryQueue(&fakeDeliverySource{claimErr: errBoom}, fixedClock{now}).
		ClaimDue(context.Background(), now, 1, entity.Lease{})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestCompleteTranslatesNoRowsToLeaseLostAndSendsUTC(t *testing.T) {
	tests := []struct {
		name    string
		srcErr  error
		wantErr error
	}{
		{"stored", nil, nil},
		{"fenced out", source.ErrNoRows, domainerrors.ErrLeaseLost},
		{"storage error", errBoom, errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeDeliverySource{completeEr: tt.srcErr}
			started := now
			d := entity.Delivery{ID: "dlv_1", Status: entity.StatusFailed, AttemptCount: 1, LastAttemptAt: &started}

			err := repositoryimpl.NewDeliveryQueue(src, fixedClock{now}).Complete(context.Background(), d, "lse_a")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			u := src.completeIn
			if u.LeaseToken != "lse_a" || u.Status != "failed" || u.NextAttemptAt != nil || u.LastAttemptAt.Location() != time.UTC || u.UpdatedAt.Location() != time.UTC {
				t.Fatalf("update = %+v", u)
			}
		})
	}
}

func TestCreateMapsWhatDidNotHappenToNull(t *testing.T) {
	tests := []struct {
		name       string
		attempt    entity.Attempt
		wantStatus *int16
		wantBody   *string
		wantError  *string
	}{
		{
			name:       "a response",
			attempt:    entity.Attempt{StatusCode: 502, Body: entity.ResponseBody{Text: "bad gateway", Truncated: true}},
			wantStatus: ptr(int16(502)), wantBody: ptr("bad gateway"),
		},
		{name: "a response with an empty body", attempt: entity.Attempt{StatusCode: 204}, wantStatus: ptr(int16(204)), wantBody: ptr("")},
		{name: "no response", attempt: entity.Attempt{Err: entity.ErrorDNS}, wantError: ptr("dns_error")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeAttemptSource{}
			tt.attempt.StartedAt = now
			tt.attempt.Duration = 1500 * time.Millisecond

			err := repositoryimpl.NewAttemptRepository(src).Create(context.Background(), tt.attempt)

			got := src.got
			if err != nil || !equalPtr(got.ResponseStatus, tt.wantStatus) || !equalPtr(got.ResponseBody, tt.wantBody) || !equalPtr(got.Error, tt.wantError) {
				t.Fatalf("model = %+v, err %v", got, err)
			}
			if got.DurationMS != 1500 || got.StartedAt.Location() != time.UTC || got.ResponseBodyTruncated != tt.attempt.Body.Truncated {
				t.Fatalf("model = %+v", got)
			}
		})
	}
}

func TestCreateReturnsSourceErrors(t *testing.T) {
	err := repositoryimpl.NewAttemptRepository(&fakeAttemptSource{err: errBoom}).Create(context.Background(), entity.Attempt{})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
