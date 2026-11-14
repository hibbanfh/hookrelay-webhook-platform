package usecase

import (
	"context"
	"fmt"
	"time"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/repository"
)

// PublishMessageDeps are the collaborators of PublishMessage.
type PublishMessageDeps struct {
	Messages  repository.MessageRepository
	Keys      repository.IdempotencyStore
	Endpoints repository.EndpointDirectory
	Outbox    repository.DeliveryOutbox
	IDs       IDGenerator
	Clock     Clock
	Tx        TxManager
}

// PublishMessage stores a message and one pending delivery per enabled endpoint in one
// transaction (POST /v1/messages). It makes no outbound call: the worker delivers later.
type PublishMessage struct {
	deps   PublishMessageDeps
	keyTTL time.Duration
}

// NewPublishMessage builds the use case. keyTTL mirrors IDEMPOTENCY_TTL.
func NewPublishMessage(deps PublishMessageDeps, keyTTL time.Duration) *PublishMessage {
	return &PublishMessage{deps: deps, keyTTL: keyTTL}
}

// PublishMessageInput is the caller-supplied data. Payload is the raw bytes the client sent.
type PublishMessageInput struct {
	ProjectID, EventType string
	Payload              []byte
	IdempotencyKey       string // raw header value
	HasIdempotencyKey    bool   // header present: an empty value is invalid, not absent
}

// PublishMessageOutput is the accepted message. Replayed is true when an earlier request with
// the same Idempotency-Key already created it.
type PublishMessageOutput struct {
	Message  entity.Message
	Replayed bool
}

// publishRequest is a validated input, ready for the transaction.
type publishRequest struct {
	msg    entity.Message
	key    entity.IdempotencyKey
	hasKey bool
}

// Execute validates everything before the transaction opens, so an invalid request never
// consumes an idempotency key or holds a connection.
func (u *PublishMessage) Execute(ctx context.Context, in PublishMessageInput) (PublishMessageOutput, error) {
	req, err := u.newRequest(in)
	if err != nil {
		return PublishMessageOutput{}, err
	}
	var out PublishMessageOutput
	err = u.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var txErr error
		out, txErr = u.publish(ctx, req)
		return txErr
	})
	if err != nil {
		return PublishMessageOutput{}, fmt.Errorf("publish message: %w", err)
	}
	return out, nil
}

func (u *PublishMessage) newRequest(in PublishMessageInput) (publishRequest, error) {
	spec := entity.MessageSpec{
		ID: u.deps.IDs.New(entity.MessageIDPrefix), ProjectID: in.ProjectID, EventType: in.EventType, Payload: in.Payload,
	}
	msg, err := entity.NewMessage(spec, u.deps.Clock.Now())
	if err != nil {
		return publishRequest{}, err
	}
	if !in.HasIdempotencyKey {
		return publishRequest{msg: msg}, nil
	}
	key, err := entity.ParseIdempotencyKey(in.IdempotencyKey)
	if err != nil {
		return publishRequest{}, err
	}
	return publishRequest{msg: msg, key: key, hasKey: true}, nil
}

// publish runs inside the transaction. The key is reserved first, so the loser of a race writes
// nothing (ADR 0013).
func (u *PublishMessage) publish(ctx context.Context, req publishRequest) (PublishMessageOutput, error) {
	replay, err := u.reserveOrReplay(ctx, req)
	if err != nil {
		return PublishMessageOutput{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	if err := u.deps.Messages.Create(ctx, req.msg); err != nil {
		return PublishMessageOutput{}, fmt.Errorf("create message: %w", err)
	}
	if err := u.fanOut(ctx, req.msg); err != nil {
		return PublishMessageOutput{}, err
	}
	return PublishMessageOutput{Message: req.msg}, nil
}

// reserveOrReplay returns nil when this request owns the key, or has none, and must publish. It
// returns the stored message when an identical earlier request owns the key.
func (u *PublishMessage) reserveOrReplay(ctx context.Context, req publishRequest) (*PublishMessageOutput, error) {
	if !req.hasKey {
		return nil, nil
	}
	record := entity.NewIdempotencyRecord(req.msg, req.key, u.keyTTL)
	won, err := u.deps.Keys.Reserve(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("reserve key: %w", err)
	}
	if won {
		return nil, nil
	}
	return u.replay(ctx, record)
}

func (u *PublishMessage) replay(ctx context.Context, wanted entity.IdempotencyRecord) (*PublishMessageOutput, error) {
	existing, err := u.deps.Keys.Find(ctx, wanted.ProjectID, wanted.Key)
	if err != nil {
		return nil, fmt.Errorf("find key: %w", err)
	}
	if !existing.Matches(wanted.Fingerprint) {
		return nil, domainerrors.ErrIdempotencyMismatch
	}
	m, err := u.deps.Messages.Get(ctx, wanted.ProjectID, existing.MessageID)
	if err != nil {
		return nil, fmt.Errorf("get message %s: %w", existing.MessageID, err)
	}
	return &PublishMessageOutput{Message: m, Replayed: true}, nil
}

// fanOut queues one delivery per endpoint that is enabled now. Endpoints registered later get
// nothing, and a project without enabled endpoints writes no delivery rows.
func (u *PublishMessage) fanOut(ctx context.Context, msg entity.Message) error {
	endpointIDs, err := u.deps.Endpoints.EnabledEndpointIDs(ctx, msg.ProjectID)
	if err != nil {
		return fmt.Errorf("list enabled endpoints: %w", err)
	}
	if len(endpointIDs) == 0 {
		return nil
	}
	deliveries := make([]entity.PendingDelivery, 0, len(endpointIDs))
	for _, endpointID := range endpointIDs {
		id := u.deps.IDs.New(entity.DeliveryIDPrefix)
		deliveries = append(deliveries, entity.NewPendingDelivery(id, msg, endpointID))
	}
	if err := u.deps.Outbox.Enqueue(ctx, deliveries); err != nil {
		return fmt.Errorf("enqueue %d deliveries: %w", len(deliveries), err)
	}
	return nil
}
