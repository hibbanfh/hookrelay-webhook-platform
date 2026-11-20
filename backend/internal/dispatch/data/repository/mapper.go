// Package repositoryimpl implements the dispatch repository ports on top of the data sources.
// It maps models to entities and translates source errors to domain errors; it contains no SQL.
package repositoryimpl

import (
	"errors"
	"time"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/dispatch/domain/entity"
)

func toClaimRequest(now time.Time, limit int, lease entity.Lease) model.ClaimRequest {
	return model.ClaimRequest{Now: now, Limit: limit, LeaseToken: lease.Token, LeaseExpiresAt: lease.ExpiresAt}
}

// toDelivery builds the claimed delivery. The claim sets next_attempt_at to the lease expiry,
// so the entity shows that value, and the lease is the one this worker asked for.
func toDelivery(m model.ClaimedDelivery, lease entity.Lease) entity.Delivery {
	expires := lease.ExpiresAt
	return entity.Delivery{
		ID: m.ID, ProjectID: m.ProjectID, MessageID: m.MessageID, EndpointID: m.EndpointID,
		Status: entity.StatusPending, Trigger: entity.Trigger(m.Trigger), AttemptCount: m.AttemptCount,
		NextAttemptAt: &expires, CreatedAt: m.CreatedAt.UTC(), Lease: lease,
	}
}

func toDeliveryUpdate(d entity.Delivery, leaseToken string, now time.Time) model.DeliveryUpdate {
	return model.DeliveryUpdate{
		ID: d.ID, LeaseToken: leaseToken, Status: string(d.Status), AttemptCount: d.AttemptCount,
		NextAttemptAt: utcPtr(d.NextAttemptAt), LastAttemptAt: utcPtr(d.LastAttemptAt), UpdatedAt: now.UTC(),
	}
}

// errNotReplayed is a programming-error guard: Delivery.Replay always sets the due time, so this
// is only reachable when a caller hands over a delivery that did not come from Replay.
var errNotReplayed = errors.New("delivery has no next attempt time to replay at")

// toReplayRequest takes the state Delivery.Replay produced. The due time doubles as updated_at.
func toReplayRequest(d entity.Delivery) (model.ReplayRequest, error) {
	if d.NextAttemptAt == nil {
		return model.ReplayRequest{}, errNotReplayed
	}
	return model.ReplayRequest{
		ID: d.ID, ProjectID: d.ProjectID, Status: string(d.Status), Trigger: string(d.Trigger),
		NextAttemptAt: d.NextAttemptAt.UTC(),
	}, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// toAttemptModel stores NULL for what did not happen: no status and no body without a response,
// no error for a response.
func toAttemptModel(a entity.Attempt) model.Attempt {
	m := model.Attempt{
		ID: a.ID, ProjectID: a.ProjectID, DeliveryID: a.DeliveryID, Number: a.Number,
		Trigger: string(a.Trigger), StartedAt: a.StartedAt.UTC(), DurationMS: a.DurationMS(),
		ResponseBodyTruncated: a.Body.Truncated,
	}
	if a.StatusCode != 0 {
		status := int16(a.StatusCode) //nolint:gosec // the sender only reports 100-599 and the column checks it
		body := a.Body.Text
		m.ResponseStatus, m.ResponseBody = &status, &body
	}
	if a.Err != entity.ErrorNone {
		errText := string(a.Err)
		m.Error = &errText
	}
	return m
}

// toDeliveryEntity maps a row the read queries returned. Timestamps come back in the local zone
// of the driver, so they are read as UTC.
func toDeliveryEntity(m model.DeliveryRow) entity.Delivery {
	return entity.Delivery{
		ID: m.ID, ProjectID: m.ProjectID, MessageID: m.MessageID, EndpointID: m.EndpointID,
		Status: entity.Status(m.Status), Trigger: entity.Trigger(m.Trigger), AttemptCount: m.AttemptCount,
		NextAttemptAt: utcPtr(m.NextAttemptAt), LastAttemptAt: utcPtr(m.LastAttemptAt), CreatedAt: m.CreatedAt.UTC(),
	}
}

// toAttempt is the inverse of toAttemptModel: NULL means the thing did not happen, so a missing
// status is 0, a missing body is the empty ResponseBody and a missing error is ErrorNone.
func toAttempt(m model.Attempt) entity.Attempt {
	a := entity.Attempt{
		ID: m.ID, ProjectID: m.ProjectID, DeliveryID: m.DeliveryID, Number: m.Number,
		Trigger: entity.Trigger(m.Trigger), StartedAt: m.StartedAt.UTC(),
		Duration: time.Duration(m.DurationMS) * time.Millisecond,
	}
	if m.ResponseStatus != nil {
		a.StatusCode = int(*m.ResponseStatus)
	}
	if m.ResponseBody != nil {
		a.Body = entity.ResponseBody{Text: *m.ResponseBody, Truncated: m.ResponseBodyTruncated}
	}
	if m.Error != nil {
		a.Err = entity.AttemptError(*m.Error)
	}
	return a
}
