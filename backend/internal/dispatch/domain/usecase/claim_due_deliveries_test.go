package usecase_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
	"hookrelay/internal/dispatch/domain/usecase"
)

var errBoom = errors.New("boom")

const leaseDuration = 30 * time.Second

func delivery(id, messageID, endpointID string) entity.Delivery {
	return entity.Delivery{ID: id, ProjectID: "proj_1", MessageID: messageID, EndpointID: endpointID, Status: entity.StatusPending}
}

func target(endpointID string) entity.Target {
	return entity.Target{EndpointID: endpointID, ProjectID: "proj_1", URL: "https://example.com/" + endpointID, Enabled: true}
}

type claimFixture struct {
	queue     *fakeQueue
	endpoints *fakeEndpoints
	messages  *fakeMessages
	uc        *usecase.ClaimDueDeliveries
}

func newClaimFixture(rows []entity.Delivery) *claimFixture {
	f := &claimFixture{
		queue: &fakeQueue{rows: rows, order: &callLog{}},
		endpoints: &fakeEndpoints{lookup: repository.TargetLookup{
			Targets: map[string]entity.Target{"ep_1": target("ep_1"), "ep_2": target("ep_2")},
		}},
		messages: &fakeMessages{payloads: map[string][]byte{"msg_1": []byte(`{"a":1}`), "msg_2": []byte(`{"a":2}`), "msg_3": []byte(`{"a":3}`)}},
	}
	f.uc = usecase.NewClaimDueDeliveries(usecase.ClaimDueDeliveriesDeps{
		Queue: f.queue, Endpoints: f.endpoints, Messages: f.messages, IDs: &sequenceIDs{}, Clock: fixedClock(t0),
	}, leaseDuration)
	return f
}

func threeDeliveries() []entity.Delivery {
	return []entity.Delivery{
		delivery("dlv_1", "msg_1", "ep_1"),
		delivery("dlv_2", "msg_2", "ep_1"),
		delivery("dlv_3", "msg_3", "ep_2"),
	}
}

func TestClaimDueDeliveriesWithoutFreeSlotsDoesNothing(t *testing.T) {
	for _, limit := range []int{0, -1} {
		f := newClaimFixture(threeDeliveries())

		out, err := f.uc.Execute(context.Background(), usecase.ClaimDueDeliveriesInput{Limit: limit})

		if err != nil || len(out.Jobs) != 0 || f.queue.claimCalls != 0 {
			t.Fatalf("limit %d: jobs %d, claim calls %d, err %v", limit, len(out.Jobs), f.queue.claimCalls, err)
		}
	}
}

func TestClaimDueDeliveriesWithNothingDueSkipsTheCatalogs(t *testing.T) {
	f := newClaimFixture(nil)

	out, err := f.uc.Execute(context.Background(), usecase.ClaimDueDeliveriesInput{Limit: 4})

	if err != nil || len(out.Jobs) != 0 || f.endpoints.calls != 0 || f.messages.calls != 0 {
		t.Fatalf("jobs %d, target reads %d, payload reads %d, err %v", len(out.Jobs), f.endpoints.calls, f.messages.calls, err)
	}
}

func TestClaimDueDeliveriesReadsEachCatalogOnceWithDistinctIDs(t *testing.T) {
	f := newClaimFixture(threeDeliveries())

	out, err := f.uc.Execute(context.Background(), usecase.ClaimDueDeliveriesInput{Limit: 5})

	if err != nil || len(out.Jobs) != 3 || len(out.Skipped) != 0 {
		t.Fatalf("jobs %d, skipped %d, err %v", len(out.Jobs), len(out.Skipped), err)
	}
	if f.endpoints.calls != 1 || !slices.Equal(f.endpoints.gotIDs, []string{"ep_1", "ep_2"}) {
		t.Fatalf("target reads %d with %v", f.endpoints.calls, f.endpoints.gotIDs)
	}
	if f.messages.calls != 1 || !slices.Equal(f.messages.gotIDs, []string{"msg_1", "msg_2", "msg_3"}) {
		t.Fatalf("payload reads %d with %v", f.messages.calls, f.messages.gotIDs)
	}
	if string(out.Jobs[2].Payload) != `{"a":3}` || out.Jobs[2].Target.EndpointID != "ep_2" {
		t.Fatalf("job 3 = %+v", out.Jobs[2])
	}
}

func TestClaimDueDeliveriesLeasesWithAFreshTokenAndTheConfiguredDuration(t *testing.T) {
	f := newClaimFixture(threeDeliveries())

	if _, err := f.uc.Execute(context.Background(), usecase.ClaimDueDeliveriesInput{Limit: 5}); err != nil {
		t.Fatal(err)
	}

	want := entity.Lease{Token: "lse_1", ExpiresAt: t0.Add(leaseDuration)}
	if f.queue.claimLease != want || !f.queue.claimNow.Equal(t0) || f.queue.claimLimit != 5 {
		t.Fatalf("claim now %v limit %d lease %+v", f.queue.claimNow, f.queue.claimLimit, f.queue.claimLease)
	}
}

func TestClaimDueDeliveriesSkipsWhatCannotBeAttempted(t *testing.T) {
	tests := []struct {
		name       string
		rows       []entity.Delivery
		unreadable map[string]string
		wantReason entity.SkipReason
		wantDetail string
	}{
		{name: "endpoint missing", rows: []entity.Delivery{delivery("dlv_x", "msg_1", "ep_gone")}, wantReason: entity.SkipTargetMissing},
		{name: "secret unreadable", rows: []entity.Delivery{delivery("dlv_x", "msg_1", "ep_1")}, unreadable: map[string]string{"ep_1": "key_version=1 cipher_open_failed"}, wantReason: entity.SkipSecretUnreadable, wantDetail: "key_version=1 cipher_open_failed"},
		{name: "payload missing", rows: []entity.Delivery{delivery("dlv_x", "msg_gone", "ep_1")}, wantReason: entity.SkipPayloadMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newClaimFixture(append(tt.rows, delivery("dlv_ok", "msg_2", "ep_2")))
			f.endpoints.lookup.Unreadable = tt.unreadable

			out, err := f.uc.Execute(context.Background(), usecase.ClaimDueDeliveriesInput{Limit: 5})

			want := []entity.Skipped{{DeliveryID: "dlv_x", Reason: tt.wantReason, Detail: tt.wantDetail}}
			if err != nil || !slices.Equal(out.Skipped, want) {
				t.Fatalf("skipped %+v, err %v", out.Skipped, err)
			}
			if len(out.Jobs) != 1 || out.Jobs[0].Delivery.ID != "dlv_ok" {
				t.Fatalf("the healthy delivery must still be returned, got %+v", out.Jobs)
			}
		})
	}
}

func TestClaimDueDeliveriesWrapsStorageErrors(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(*claimFixture)
	}{
		{"claim fails", func(f *claimFixture) { f.queue.claimErr = errBoom }},
		{"targets fail", func(f *claimFixture) { f.endpoints.err = errBoom }},
		{"payloads fail", func(f *claimFixture) { f.messages.err = errBoom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newClaimFixture(threeDeliveries())
			tt.break_(f)

			_, err := f.uc.Execute(context.Background(), usecase.ClaimDueDeliveriesInput{Limit: 5})

			if !errors.Is(err, errBoom) {
				t.Fatalf("got %v, want the storage error wrapped", err)
			}
		})
	}
}
