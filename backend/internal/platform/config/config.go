// Package config parses and validates all process configuration from the
// environment once at startup. Invalid configuration is a startup error.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Environment names accepted by APP_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

const (
	requiredKeyBytes = 32
	leaseMargin      = 5 * time.Second
	// The poll interval is the first-delivery latency (the US-004 budget is 5 s) and, when idle,
	// the number of claim queries per second. Outside this range it is a mistake in either direction.
	minPollInterval = 50 * time.Millisecond
	maxPollInterval = 2 * time.Second
	// minIdempotencyTTL keeps expires_at strictly after created_at once both are stored with microsecond precision.
	minIdempotencyTTL = time.Second
	seedKeyLength     = 46 // "hr_" + 43 base64url characters
)

// WorkerHeartbeatMaxAge is how long the worker may go without a claim or a finished attempt
// before its health endpoint fails. DELIVERY_TIMEOUT plus the completion budget must fit in it.
const WorkerHeartbeatMaxAge = 30 * time.Second

// WorkerCompletionTimeout bounds recording an attempt (the completion transaction). It is the
// one value behind the shutdown budget (shutdown timeout + this) and the heartbeat window.
const WorkerCompletionTimeout = 3 * time.Second

// Config is the typed, validated process configuration (architecture section 14).
type Config struct {
	AppEnv                  string
	HTTPAddr                string
	DatabaseURL             string
	LogLevel                slog.Level
	PublicBaseURL           string
	ProjectCreationEnabled  bool
	SecretEncryptionKey     []byte
	SecretEncryptionKeyPrev []byte
	MaxEndpointsPerProject  int
	MaxBodyBytes            int64
	IdempotencyTTL          time.Duration
	DeliveryTimeout         time.Duration
	RetrySchedule           []time.Duration
	RetryJitter             float64
	WorkerConcurrency       int
	WorkerPollInterval      time.Duration
	WorkerLeaseDuration     time.Duration
	WorkerShutdownTimeout   time.Duration
	WorkerHealthAddr        string
	DemoReceiverEnabled     bool
	// DemoReceiverBaseURL must be reachable from the worker. When the receiver is enabled it is the
	// only allowlisted (SSRF-exempt) destination.
	DemoReceiverBaseURL string
	// OutboundAllowlist is derived, never read from the environment: DemoReceiverBaseURL when the
	// receiver is enabled, empty otherwise (ADR 0017).
	OutboundAllowlist     []string
	OutboundAllowlistOnly bool
	SeedDemoAPIKey        string
	SeedDemoProjectName   string

	NeonAuthBaseURL       string
	NeonAuthSessionCookie string
	DashboardOrigin       string
	NeonAuthTimeout       time.Duration
	SessionCacheTTL       time.Duration
}

// IsProduction reports whether APP_ENV is production.
func (c Config) IsProduction() bool { return c.AppEnv == EnvProduction }

// Getenv abstracts os.Getenv so tests can inject values.
type Getenv func(string) string

// Load reads and validates the configuration. All problems are reported together.
func Load(getenv Getenv) (Config, error) {
	r := &reader{get: getenv}
	appEnv := r.oneOf("APP_ENV", EnvDevelopment, EnvDevelopment, EnvProduction)
	publicURL := r.str("PUBLIC_BASE_URL", "http://localhost:8080")
	demoEnabled := r.boolean("DEMO_RECEIVER_ENABLED", true)
	demoBase := r.str(envDemoBaseURL, strings.TrimRight(publicURL, "/")+demoReceiverPath)
	r.removed(envRemovedAllowlist, removedAllowlistReason)

	neon := r.neonAuthSettings()

	cfg := Config{
		AppEnv:                  appEnv,
		HTTPAddr:                r.str("HTTP_ADDR", ":8080"),
		DatabaseURL:             r.required("DATABASE_URL"),
		LogLevel:                r.level("LOG_LEVEL", "info"),
		PublicBaseURL:           publicURL,
		ProjectCreationEnabled:  r.boolean("PROJECT_CREATION_ENABLED", appEnv != EnvProduction),
		SecretEncryptionKey:     r.key("SECRET_ENCRYPTION_KEY", true),
		SecretEncryptionKeyPrev: r.key("SECRET_ENCRYPTION_KEY_PREVIOUS", false),
		MaxEndpointsPerProject:  r.positiveInt("MAX_ENDPOINTS_PER_PROJECT", 20),
		MaxBodyBytes:            int64(r.positiveInt("MAX_BODY_BYTES", 262144)),
		IdempotencyTTL:          r.duration("IDEMPOTENCY_TTL", "24h"),
		DeliveryTimeout:         r.duration("DELIVERY_TIMEOUT", "10s"),
		RetrySchedule:           r.retrySchedule("RETRY_SCHEDULE", "5s,30s,2m,15m,1h"),
		RetryJitter:             r.jitter("RETRY_JITTER", "0.10"),
		WorkerConcurrency:       r.positiveInt("WORKER_CONCURRENCY", 16),
		WorkerPollInterval:      r.duration("WORKER_POLL_INTERVAL", "500ms"),
		WorkerLeaseDuration:     r.duration("WORKER_LEASE_DURATION", "30s"),
		WorkerShutdownTimeout:   r.duration("WORKER_SHUTDOWN_TIMEOUT", "25s"),
		WorkerHealthAddr:        r.str("WORKER_HEALTH_ADDR", ":9091"),
		DemoReceiverEnabled:     demoEnabled,
		DemoReceiverBaseURL:     demoBase,
		OutboundAllowlist:       demoAllowlist(demoEnabled, demoBase),
		OutboundAllowlistOnly:   r.boolean(envAllowlistOnly, false),
		SeedDemoAPIKey:          r.str("SEED_DEMO_API_KEY", ""),
		SeedDemoProjectName:     r.str("SEED_DEMO_PROJECT_NAME", "Hookrelay Demo"),
		NeonAuthBaseURL:         neon.baseURL,
		NeonAuthSessionCookie:   neon.cookie,
		DashboardOrigin:         neon.origin,
		NeonAuthTimeout:         neon.timeout,
		SessionCacheTTL:         neon.cacheTTL,
	}
	r.add(validateCross(cfg))
	r.add(validateDemoReceiver(cfg))
	r.add(validateNeonAuth(cfg))
	if err := errors.Join(r.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// validateCross checks rules that span several variables.
func validateCross(c Config) error {
	var errs []error
	if c.WorkerLeaseDuration <= c.DeliveryTimeout+leaseMargin {
		errs = append(errs, errors.New("WORKER_LEASE_DURATION must be greater than DELIVERY_TIMEOUT + 5s"))
	}
	if c.WorkerPollInterval < minPollInterval || c.WorkerPollInterval > maxPollInterval {
		errs = append(errs, errors.New("WORKER_POLL_INTERVAL must be between 50ms and 2s"))
	}
	if c.DeliveryTimeout+WorkerCompletionTimeout >= WorkerHeartbeatMaxAge {
		errs = append(errs, errors.New("DELIVERY_TIMEOUT + 3s must be below the 30s worker heartbeat window, or a healthy saturated worker reports unhealthy"))
	}
	if c.IdempotencyTTL < minIdempotencyTTL {
		errs = append(errs, errors.New("IDEMPOTENCY_TTL must be at least 1s"))
	}
	if c.IsProduction() && !strings.HasPrefix(c.PublicBaseURL, "https://") {
		errs = append(errs, errors.New("PUBLIC_BASE_URL must start with https:// in production"))
	}
	if _, err := url.ParseRequestURI(c.PublicBaseURL); err != nil {
		errs = append(errs, fmt.Errorf("PUBLIC_BASE_URL is not a valid URL: %w", err))
	}
	return errors.Join(errs...)
}

// ValidateSeed checks the settings only the seed subcommand needs.
func (c Config) ValidateSeed() error {
	if !strings.HasPrefix(c.SeedDemoAPIKey, "hr_") || len(c.SeedDemoAPIKey) != seedKeyLength {
		return errors.New("SEED_DEMO_API_KEY must be hr_ followed by 43 base64url characters")
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// reader accumulates parse errors so Load can report every problem at once.
type reader struct {
	get  Getenv
	errs []error
}

func (r *reader) add(err error) {
	if err != nil {
		r.errs = append(r.errs, err)
	}
}

func (r *reader) fail(name, format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf("%s: %s", name, fmt.Sprintf(format, args...)))
}

func (r *reader) str(name, def string) string {
	v := strings.TrimSpace(r.get(name))
	if v == "" {
		return def
	}
	return v
}

// removed fails when a variable that no longer exists is still set, naming why.
func (r *reader) removed(name, reason string) {
	if r.str(name, "") != "" {
		r.fail(name, "%s", reason)
	}
}

func (r *reader) required(name string) string {
	v := r.str(name, "")
	if v == "" {
		r.fail(name, "is required")
	}
	return v
}

func (r *reader) oneOf(name, def string, allowed ...string) string {
	v := r.str(name, def)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	r.fail(name, "must be one of %s, got %q", strings.Join(allowed, "|"), v)
	return def
}

func (r *reader) level(name, def string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(r.str(name, def))); err != nil {
		r.fail(name, "must be debug|info|warn|error")
	}
	return l
}

func (r *reader) boolean(name string, def bool) bool {
	v := r.str(name, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.fail(name, "must be a boolean, got %q", v)
	}
	return b
}

func (r *reader) positiveInt(name string, def int) int {
	v := r.str(name, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		r.fail(name, "must be a positive integer, got %q", v)
	}
	return n
}

func (r *reader) duration(name, def string) time.Duration {
	v := r.str(name, def)
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		r.fail(name, "must be a positive Go duration, got %q", v)
	}
	return d
}

// maxRetryDelay mirrors entity.MaxRetryDelay (platform must not import a module): it catches typos
// like 1000h and keeps the jitter multiplication far from time.Duration overflow.
const maxRetryDelay = 24 * time.Hour

func (r *reader) retrySchedule(name, def string) []time.Duration {
	parts := splitList(r.str(name, def))
	if len(parts) == 0 {
		r.fail(name, "must list at least one positive Go duration")
		return nil
	}
	out := make([]time.Duration, 0, len(parts))
	for _, p := range parts {
		d, err := parseRetryDelay(p)
		if err != nil {
			r.fail(name, "%v", err)
			continue
		}
		out = append(out, d)
	}
	return out
}

func parseRetryDelay(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	if d > maxRetryDelay {
		return 0, fmt.Errorf("%q exceeds %s", s, maxRetryDelay)
	}
	return d, nil
}

func (r *reader) jitter(name, def string) float64 {
	v := r.str(name, def)
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || !(f >= 0 && f < 1) { // written this way so NaN fails
		r.fail(name, "must satisfy 0 <= j < 1, got %q", v)
	}
	return f
}

func (r *reader) key(name string, required bool) []byte {
	v := r.str(name, "")
	if v == "" && required {
		r.fail(name, "is required")
	}
	if v == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(b) != requiredKeyBytes {
		r.fail(name, "must be base64 encoding of exactly %d bytes", requiredKeyBytes)
	}
	return b
}
