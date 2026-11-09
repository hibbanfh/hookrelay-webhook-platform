package config_test

import (
	"strings"
	"testing"
	"time"

	"hookrelay/internal/platform/config"
)

const neonBase = "https://ep-x.neonauth.c-8.us-east-2.aws.neon.tech/neondb/auth"

func TestLoadNeonAuth(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{name: "development without neon is allowed", mutate: func(map[string]string) {}},
		{name: "development with http base url is allowed", mutate: func(m map[string]string) {
			m["NEON_AUTH_BASE_URL"] = "http://localhost:9999/auth"
			m["DASHBOARD_ORIGIN"] = "http://localhost:3000"
		}},
		{name: "https base url with origin", mutate: func(m map[string]string) {
			m["NEON_AUTH_BASE_URL"] = neonBase
			m["DASHBOARD_ORIGIN"] = "http://localhost:3000"
		}},
		{name: "base url without origin", mutate: func(m map[string]string) { m["NEON_AUTH_BASE_URL"] = neonBase }, wantErr: "DASHBOARD_ORIGIN"},
		{name: "base url that is not absolute", mutate: func(m map[string]string) {
			m["NEON_AUTH_BASE_URL"] = "/auth"
			m["DASHBOARD_ORIGIN"] = "http://localhost:3000"
		}, wantErr: "NEON_AUTH_BASE_URL"},
		{name: "origin with a path", mutate: func(m map[string]string) {
			m["NEON_AUTH_BASE_URL"] = neonBase
			m["DASHBOARD_ORIGIN"] = "http://localhost:3000/app"
		}, wantErr: "DASHBOARD_ORIGIN"},
		{name: "origin with a trailing slash", mutate: func(m map[string]string) {
			m["NEON_AUTH_BASE_URL"] = neonBase
			m["DASHBOARD_ORIGIN"] = "http://localhost:3000/"
		}, wantErr: "DASHBOARD_ORIGIN"},
		{name: "origin without scheme", mutate: func(m map[string]string) {
			m["NEON_AUTH_BASE_URL"] = neonBase
			m["DASHBOARD_ORIGIN"] = "localhost:3000"
		}, wantErr: "DASHBOARD_ORIGIN"},
		{name: "production requires the base url", mutate: productionEnv, wantErr: "NEON_AUTH_BASE_URL"},
		{name: "production requires https", mutate: func(m map[string]string) {
			productionEnv(m)
			m["NEON_AUTH_BASE_URL"] = "http://auth.example.com"
			m["DASHBOARD_ORIGIN"] = "https://app.example.com"
		}, wantErr: "NEON_AUTH_BASE_URL must start with https://"},
		{name: "production with https", mutate: func(m map[string]string) {
			productionEnv(m)
			m["NEON_AUTH_BASE_URL"] = neonBase
			m["DASHBOARD_ORIGIN"] = "https://app.example.com"
		}},
		{name: "production requires an https dashboard origin", mutate: func(m map[string]string) {
			productionEnv(m)
			m["NEON_AUTH_BASE_URL"] = neonBase
			m["DASHBOARD_ORIGIN"] = "http://app.example.com"
		}, wantErr: "DASHBOARD_ORIGIN must start with https://"},
		{name: "timeout must be shorter than the request timeout", mutate: func(m map[string]string) { m["NEON_AUTH_TIMEOUT"] = "10s" }, wantErr: "NEON_AUTH_TIMEOUT"},
		{name: "timeout must be positive", mutate: func(m map[string]string) { m["NEON_AUTH_TIMEOUT"] = "0s" }, wantErr: "NEON_AUTH_TIMEOUT"},
		{name: "cache ttl zero turns the cache off", mutate: func(m map[string]string) { m["SESSION_CACHE_TTL"] = "0s" }},
		{name: "cache ttl at the 60s limit", mutate: func(m map[string]string) { m["SESSION_CACHE_TTL"] = "60s" }},
		{name: "cache ttl above 60s", mutate: func(m map[string]string) { m["SESSION_CACHE_TTL"] = "61s" }, wantErr: "SESSION_CACHE_TTL"},
		{name: "cache ttl negative", mutate: func(m map[string]string) { m["SESSION_CACHE_TTL"] = "-1s" }, wantErr: "SESSION_CACHE_TTL"},
		{name: "cache ttl garbage", mutate: func(m map[string]string) { m["SESSION_CACHE_TTL"] = "soon" }, wantErr: "SESSION_CACHE_TTL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validEnv()
			tt.mutate(m)

			_, err := config.Load(env(m))

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func productionEnv(m map[string]string) {
	m["APP_ENV"] = "production"
	m["PUBLIC_BASE_URL"] = "https://api.example.com"
}

func TestLoadNeonAuthDefaults(t *testing.T) {
	cfg, err := config.Load(env(validEnv()))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.NeonAuthEnabled() || cfg.NeonAuthBaseURL != "" {
		t.Fatalf("neon must be off without a base url: %+v", cfg)
	}
	if cfg.NeonAuthSessionCookie != "__Secure-neon-auth.session_token" {
		t.Fatalf("cookie = %q", cfg.NeonAuthSessionCookie)
	}
	if cfg.NeonAuthTimeout != 3*time.Second || cfg.SessionCacheTTL != 5*time.Second {
		t.Fatalf("timeout %v ttl %v", cfg.NeonAuthTimeout, cfg.SessionCacheTTL)
	}
}

func TestLoadNeonAuthReadsValues(t *testing.T) {
	m := validEnv()
	m["NEON_AUTH_BASE_URL"] = neonBase + "/"
	m["DASHBOARD_ORIGIN"] = "https://app.example.com"
	m["NEON_AUTH_SESSION_COOKIE"] = "custom.cookie"

	cfg, err := config.Load(env(m))

	if err != nil || !cfg.NeonAuthEnabled() || cfg.NeonAuthBaseURL != neonBase || cfg.DashboardOrigin != "https://app.example.com" || cfg.NeonAuthSessionCookie != "custom.cookie" {
		t.Fatalf("cfg %+v err %v", cfg, err)
	}
}
