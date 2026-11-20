package wire

import (
	"fmt"
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	dispatchsource "hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/data/webhookhttp"
	dispatchhttp "hookrelay/internal/dispatch/delivery/presenter/http"
	"hookrelay/internal/dispatch/delivery/worker"
	dispatchentity "hookrelay/internal/dispatch/domain/entity"
	dispatchrepo "hookrelay/internal/dispatch/domain/repository"
	dispatchuc "hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/netguard"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/library/random"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/httpx"
)

// DispatchSet builds the dispatch module for the worker process: queue, sender, use cases and
// the runner. It needs the catalog use cases (EndpointTargetsSet, MessagePayloadsSet), the
// netguard options and the build version from the injector. The API graph never includes it
// (ADR 0002): a request path must not be able to make an outbound call.
var DispatchSet = wire.NewSet(
	ProvideDispatchClock, ProvideQueueClock, ProvideDispatchIDs, ProvideDispatchTx, ProvideDispatchJitter,

	ProvideDeliverySource, ProvideAttemptSource,
	wire.Bind(new(dispatchsource.DeliveryPersistent), new(*dispatchsource.DeliveryPersistentImpl)),
	wire.Bind(new(dispatchsource.AttemptPersistent), new(*dispatchsource.AttemptPersistentImpl)),
	repositoryimpl.NewDeliveryQueue,
	repositoryimpl.NewAttemptRepository,

	ProvideRetryPlanner,

	netguard.ProviderSet, UserAgent, ProvideSender,
	wire.Bind(new(dispatchrepo.Sender), new(*webhookhttp.Sender)),
	ProvideEndpointCatalog, ProvideMessageCatalog,

	wire.Struct(new(dispatchuc.ClaimDueDeliveriesDeps), "*"),
	ProvideClaimDueDeliveries,
	wire.Struct(new(dispatchuc.AttemptDeliveryDeps), "*"),
	ProvideAttemptDelivery,

	ProvideHeartbeat, WorkerOptions,
	wire.Bind(new(worker.Claimer), new(*dispatchuc.ClaimDueDeliveries)),
	wire.Bind(new(worker.Attempter), new(*dispatchuc.AttemptDelivery)),
	wire.Struct(new(worker.Deps), "*"),
	worker.NewRunner,
)

// ProvideDeliverySource hands the pool to the delivery source as the Querier it needs.
func ProvideDeliverySource(pool *pgxpool.Pool) *dispatchsource.DeliveryPersistentImpl {
	return dispatchsource.NewDeliveryPersistent(pool)
}

// ProvideAttemptSource hands the pool to the attempt source.
func ProvideAttemptSource(pool *pgxpool.Pool) *dispatchsource.AttemptPersistentImpl {
	return dispatchsource.NewAttemptPersistent(pool)
}

// ProvideRetryPlanner builds the backoff schedule from RETRY_SCHEDULE and RETRY_JITTER.
func ProvideRetryPlanner(cfg config.Config, src dispatchentity.JitterSource) (dispatchentity.RetryPlanner, error) {
	schedule, err := dispatchentity.NewBackoffSchedule(cfg.RetrySchedule, cfg.RetryJitter, src)
	if err != nil {
		return nil, fmt.Errorf("build retry schedule: %w", err)
	}
	return schedule, nil
}

// ProvideSender builds the outbound sender with two clients: the guarded one for ordinary
// destinations (SSRF checks at dial time in production) and the allowlisted one for the demo
// receiver, which may live on a private address.
func ProvideSender(policy *netguard.Policy, cfg config.Config, ua webhookhttp.UserAgent) *webhookhttp.Sender {
	return webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded:       webhookhttp.NewClient(policy.GuardedDial(), cfg.WorkerConcurrency),
		Allowlisted:   webhookhttp.NewClient(policy.AllowlistedDial(), cfg.WorkerConcurrency),
		IsAllowlisted: policy.IsAllowlisted,
		UserAgent:     ua,
		Timeout:       cfg.DeliveryTimeout,
	})
}

// ProvideClaimDueDeliveries builds the use case with the WORKER_LEASE_DURATION lease.
func ProvideClaimDueDeliveries(deps dispatchuc.ClaimDueDeliveriesDeps, cfg config.Config) *dispatchuc.ClaimDueDeliveries {
	return dispatchuc.NewClaimDueDeliveries(deps, cfg.WorkerLeaseDuration)
}

// ProvideAttemptDelivery builds the use case with the completion time bound.
func ProvideAttemptDelivery(deps dispatchuc.AttemptDeliveryDeps) *dispatchuc.AttemptDelivery {
	return dispatchuc.NewAttemptDelivery(deps, config.WorkerCompletionTimeout)
}

// ProvideHeartbeat builds the heartbeat the runner beats and the worker health endpoint reads.
func ProvideHeartbeat(c clock.System) *worker.Heartbeat {
	return worker.NewHeartbeat(c.Now, config.WorkerHeartbeatMaxAge)
}

// The providers below satisfy the ports of the dispatch module from the shared libraries, as
// functions instead of wire.Bind for the reason recorded in endpoint.go.

// ProvideDispatchClock binds the system clock to the dispatch Clock port.
func ProvideDispatchClock(c clock.System) dispatchuc.Clock { return c }

// ProvideQueueClock binds the system clock to the Clock port of the queue repository.
func ProvideQueueClock(c clock.System) repositoryimpl.Clock { return c }

// ProvideDispatchIDs binds the ULID generator to the dispatch IDGenerator port.
func ProvideDispatchIDs(g *ids.Generator) dispatchuc.IDGenerator { return g }

// ProvideDispatchTx binds the transaction manager to the dispatch TxManager port.
func ProvideDispatchTx(m *postgres.TxManager) dispatchuc.TxManager { return m }

// ProvideDispatchJitter binds the uniform random source to the dispatch JitterSource port.
func ProvideDispatchJitter(u random.Uniform) dispatchentity.JitterSource { return u }

// DispatchAPISet builds the API side of the dispatch module: the read routes, the replay write
// and the two internal reads the message module goes through an adapter (ADR 0015, ADR 0016). It
// has no sender, no worker and no queue, so a request path cannot make an outbound call (ADR
// 0002). It needs GetEndpoint from EndpointSet for the ownership and enabled checks. It shares
// ProvideDispatchClock with DispatchSet; the two sets are never in the same injector.
var DispatchAPISet = wire.NewSet(
	ProvideDeliveryReadSource, ProvideAttemptSource, ProvideDeliveryReplaySource,
	wire.Bind(new(dispatchsource.DeliveryReadPersistent), new(*dispatchsource.DeliveryReadPersistentImpl)),
	wire.Bind(new(dispatchsource.AttemptListPersistent), new(*dispatchsource.AttemptPersistentImpl)),
	wire.Bind(new(dispatchsource.DeliveryReplayPersistent), new(*dispatchsource.DeliveryReplayPersistentImpl)),
	repositoryimpl.NewDeliveryReader,
	repositoryimpl.NewAttemptReader,
	repositoryimpl.NewDeliveryReplayer,
	ProvideEndpointStateReader,
	ProvideDispatchClock,

	dispatchuc.NewListEndpointDeliveries,
	dispatchuc.NewListDeliveryAttempts,
	dispatchuc.NewSummarizeDeliveries,
	dispatchuc.NewListMessageDeliveries,
	dispatchuc.NewReplayDelivery,

	ProvideDispatchHandler,
	wire.Struct(new(DispatchRoutes), "*"),
)

// DispatchRoutes is everything the router needs to register the dispatch routes. The guard is
// the project module function type, so the dispatch module never imports the project module.
type DispatchRoutes struct {
	Handler *dispatchhttp.Handler
	Guard   ProjectGuard
}

// Register adds the dispatch routes to mux.
func (d DispatchRoutes) Register(mux *http.ServeMux) {
	dispatchhttp.Routes(mux, d.Handler, d.Guard)
}

// ProvideDeliveryReadSource hands the pool to the delivery read source as the Querier it needs.
func ProvideDeliveryReadSource(pool *pgxpool.Pool) *dispatchsource.DeliveryReadPersistentImpl {
	return dispatchsource.NewDeliveryReadPersistent(pool)
}

// ProvideDeliveryReplaySource hands the pool to the replay source as the Querier it needs.
func ProvideDeliveryReplaySource(pool *pgxpool.Pool) *dispatchsource.DeliveryReplayPersistentImpl {
	return dispatchsource.NewDeliveryReplayPersistent(pool)
}

// ProvideDispatchHandler builds the HTTP handler from the three route use cases.
func ProvideDispatchHandler(
	deliveries *dispatchuc.ListEndpointDeliveries, attempts *dispatchuc.ListDeliveryAttempts,
	replay *dispatchuc.ReplayDelivery, resp httpx.Responder,
) *dispatchhttp.Handler {
	return dispatchhttp.NewHandler(
		dispatchhttp.UseCases{EndpointDeliveries: deliveries, Attempts: attempts, Replay: replay}, resp)
}
