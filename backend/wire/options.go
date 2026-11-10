package wire

import (
	"net"
	"os"

	"hookrelay/internal/library/httpclient"
	"hookrelay/internal/library/logger"
	"hookrelay/internal/library/netguard"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/library/secretbox"
	"hookrelay/internal/platform/config"
)

// ProcessName tags every log line of a process. Each injector supplies its own.
type ProcessName string

// PoolSize is the maximum number of database connections of a process.
type PoolSize int32

// Process names and pool sizes of the subcommands.
const (
	APIProcess    ProcessName = "api"
	WorkerProcess ProcessName = "worker"
	SeedProcess   ProcessName = "seed"

	APIPoolSize    PoolSize = 10
	WorkerPoolSize PoolSize = 6
	SeedPoolSize   PoolSize = 2
)

// LoggerOptions maps the configuration to the logger library.
func LoggerOptions(cfg config.Config, process ProcessName) logger.Options {
	return logger.Options{Writer: os.Stdout, Level: cfg.LogLevel, Process: string(process)}
}

// PostgresOptions maps the configuration to the Postgres library.
func PostgresOptions(cfg config.Config, size PoolSize) postgres.Options {
	return postgres.Options{DSN: cfg.DatabaseURL, MaxConns: int32(size)}
}

// HTTPClientOptions maps the configuration to the outbound client used for Neon Auth.
func HTTPClientOptions(cfg config.Config) httpclient.Options {
	return httpclient.Options{Timeout: cfg.NeonAuthTimeout}
}

// SecretboxOptions maps the configuration to the secret box.
func SecretboxOptions(cfg config.Config) secretbox.Options {
	return secretbox.Options{Key: cfg.SecretEncryptionKey, PreviousKey: cfg.SecretEncryptionKeyPrev}
}

// NetguardOptions maps the configuration to the SSRF policy. The resolver is injected here
// so the policy never reaches for a global.
func NetguardOptions(cfg config.Config) netguard.Options {
	return netguard.Options{
		Production:    cfg.IsProduction(),
		Allowlist:     cfg.OutboundAllowlist,
		AllowlistOnly: cfg.OutboundAllowlistOnly,
		Resolver:      net.DefaultResolver,
	}
}
