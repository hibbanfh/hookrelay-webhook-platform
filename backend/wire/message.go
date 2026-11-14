package wire

import (
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/postgres"
	messagerepo "hookrelay/internal/message/data/repository"
	messagesource "hookrelay/internal/message/data/source"
	messagehttp "hookrelay/internal/message/delivery/presenter/http"
	messageuc "hookrelay/internal/message/domain/usecase"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/httpx"
)

// MessageRoutes is everything the router needs to register the message routes. The guard is
// the project module function type, so the message module never imports the project module.
type MessageRoutes struct {
	Handler *messagehttp.Handler
	Guard   ProjectGuard
}

// Register adds the message routes to mux.
func (m MessageRoutes) Register(mux *http.ServeMux) {
	messagehttp.Routes(mux, m.Handler, m.Guard)
}

// MessageSet builds the message module and binds the libraries to its ports. The message module
// reads endpoints and appends deliveries through its own repositories (ADR 0013), so it needs no
// adapter in adapters.go.
var MessageSet = wire.NewSet(
	ProvideMessageClock, ProvideMessageIDs, ProvideMessageTx,

	ProvideMessageSource, ProvideIdempotencySource, ProvideFanoutSource,
	wire.Bind(new(messagesource.MessagePersistent), new(*messagesource.MessagePersistentImpl)),
	wire.Bind(new(messagesource.IdempotencyPersistent), new(*messagesource.IdempotencyPersistentImpl)),
	wire.Bind(new(messagesource.FanoutPersistent), new(*messagesource.FanoutPersistentImpl)),
	messagerepo.NewMessageRepository,
	messagerepo.NewIdempotencyStore,
	messagerepo.NewEndpointDirectory,
	messagerepo.NewDeliveryOutbox,

	wire.Struct(new(messageuc.PublishMessageDeps), "*"),
	ProvidePublishMessage,
	ProvideMessageHandler,
	wire.Struct(new(MessageRoutes), "*"),
)

// ProvideMessageSource hands the pool to the message source as the Querier it needs.
func ProvideMessageSource(pool *pgxpool.Pool) *messagesource.MessagePersistentImpl {
	return messagesource.NewMessagePersistent(pool)
}

// ProvideIdempotencySource hands the pool to the idempotency source.
func ProvideIdempotencySource(pool *pgxpool.Pool) *messagesource.IdempotencyPersistentImpl {
	return messagesource.NewIdempotencyPersistent(pool)
}

// ProvideFanoutSource hands the pool to the fan-out source.
func ProvideFanoutSource(pool *pgxpool.Pool) *messagesource.FanoutPersistentImpl {
	return messagesource.NewFanoutPersistent(pool)
}

// ProvidePublishMessage builds the use case with the IDEMPOTENCY_TTL window.
func ProvidePublishMessage(deps messageuc.PublishMessageDeps, cfg config.Config) *messageuc.PublishMessage {
	return messageuc.NewPublishMessage(deps, cfg.IdempotencyTTL)
}

// ProvideMessageHandler builds the handler from the use case.
func ProvideMessageHandler(publish *messageuc.PublishMessage, resp httpx.Responder) *messagehttp.Handler {
	return messagehttp.NewHandler(messagehttp.UseCases{Publish: publish}, resp)
}

// The three providers below satisfy the ports of the message module from the shared libraries,
// as functions instead of wire.Bind for the reason recorded in endpoint.go.

// ProvideMessageClock binds the system clock to the message Clock port.
func ProvideMessageClock(c clock.System) messageuc.Clock { return c }

// ProvideMessageIDs binds the ULID generator to the message IDGenerator port.
func ProvideMessageIDs(g *ids.Generator) messageuc.IDGenerator { return g }

// ProvideMessageTx binds the transaction manager to the message TxManager port.
func ProvideMessageTx(m *postgres.TxManager) messageuc.TxManager { return m }
