package wire

import (
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	dispatchsource "hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/data/webhookhttp"
	"hookrelay/internal/dispatch/delivery/worker"
	dispatchentity "hookrelay/internal/dispatch/domain/entity"
	dispatchrepo "hookrelay/internal/dispatch/domain/repository"
	dispatchuc "hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/netguard"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/platform/config"
)

// DispatchSet builds the dispatch module for the worker process: queue, sender, use cases and
// the runner. It needs the catalog use cases (EndpointTargetsSet, MessagePayloadsSet), the
// netguard options and the build version from the injector. The API graph never includes it
// (ADR 0002): a request path must not be able to make an outbound call.
var DispatchSet = wire.NewSet(
	ProvideDispatchClock, ProvideQueueClock, ProvideDispatchIDs, ProvideDispatchTx,

	ProvideDeliverySource, ProvideAttemptSource,
	wire.Bind(new(dispatchsource.DeliveryPersistent), new(*dispatchsource.DeliveryPersistentImpl)),
	wire.Bind(new(dispatchsource.AttemptPersistent), new(*dispatchsource.AttemptPersistentImpl)),
	repositoryimpl.NewDeliveryQueue,
	repositoryimpl.NewAttemptRepository,

	// US-005 replaces this one provider with the backoff schedule.
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

// ProvideRetryPlanner returns the no-retry planner: until US-005 a failed attempt is final.
func ProvideRetryPlanner() dispatchentity.RetryPlanner { return dispatchentity.NoRetry{} }

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
