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

// messageDataSet is the message storage the API and the worker graph share.
var messageDataSet = wire.NewSet(
	ProvideMessageSource,
	wire.Bind(new(messagesource.MessagePersistent), new(*messagesource.MessagePersistentImpl)),
	messagerepo.NewMessageRepository,
)

// MessagePayloadsSet is what the worker needs from the message module: the internal use case
// that reads payloads by ID (ADR 0014 section 4).
var MessagePayloadsSet = wire.NewSet(messageDataSet, messageuc.NewGetMessagePayloads)

// MessageSet builds the message module and binds the libraries to its ports. The message module
// reads endpoints and appends deliveries through its own repositories (ADR 0013). It shows
// deliveries it does not own through ProvideDeliveryReader, an adapter over dispatch (ADR 0015).
var MessageSet = wire.NewSet(
	messageDataSet,
	ProvideMessageClock, ProvideMessageIDs, ProvideMessageTx,

	ProvideIdempotencySource, ProvideFanoutSource,
	wire.Bind(new(messagesource.IdempotencyPersistent), new(*messagesource.IdempotencyPersistentImpl)),
	wire.Bind(new(messagesource.FanoutPersistent), new(*messagesource.FanoutPersistentImpl)),
	messagerepo.NewIdempotencyStore,
	messagerepo.NewEndpointDirectory,
	messagerepo.NewDeliveryOutbox,

	wire.Struct(new(messageuc.PublishMessageDeps), "*"),
	ProvidePublishMessage,
	messageuc.NewListMessages,
	messageuc.NewGetMessage,
	ProvideDeliveryReader,
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

// ProvideMessageHandler builds the handler from the three route use cases.
func ProvideMessageHandler(
	publish *messageuc.PublishMessage, list *messageuc.ListMessages, get *messageuc.GetMessage, resp httpx.Responder,
) *messagehttp.Handler {
	return messagehttp.NewHandler(messagehttp.UseCases{Publish: publish, List: list, Get: get}, resp)
}

// The three providers below satisfy the ports of the message module from the shared libraries,
// as functions instead of wire.Bind for the reason recorded in endpoint.go.

// ProvideMessageClock binds the system clock to the message Clock port.
func ProvideMessageClock(c clock.System) messageuc.Clock { return c }

// ProvideMessageIDs binds the ULID generator to the message IDGenerator port.
func ProvideMessageIDs(g *ids.Generator) messageuc.IDGenerator { return g }

// ProvideMessageTx binds the transaction manager to the message TxManager port.
func ProvideMessageTx(m *postgres.TxManager) messageuc.TxManager { return m }
