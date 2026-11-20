package config_test

import (
	"slices"
	"strings"
	"testing"

	"hookrelay/internal/platform/config"
)

// productionDemoEnv is a valid production environment: https public URL and Neon Auth.
func productionDemoEnv(m map[string]string) {
	productionEnv(m)
	m["NEON_AUTH_BASE_URL"] = neonBase
	m["DASHBOARD_ORIGIN"] = "https://app.example.com"
}

func TestLoadDerivesTheDemoReceiverAndTheAllowlist(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(map[string]string)
		wantBase      string
		wantAllowlist []string
	}{
		{
			name: "development default", mutate: func(map[string]string) {},
			wantBase: "http://localhost:8080/demo/receiver", wantAllowlist: []string{"http://localhost:8080/demo/receiver"},
		},
		{
			name: "production default follows the public url", mutate: productionDemoEnv,
			wantBase: "https://api.example.com/demo/receiver", wantAllowlist: []string{"https://api.example.com/demo/receiver"},
		},
		{
			name: "a trailing slash on the public url is dropped", mutate: func(m map[string]string) { m["PUBLIC_BASE_URL"] = "http://localhost:8080/" },
			wantBase: "http://localhost:8080/demo/receiver", wantAllowlist: []string{"http://localhost:8080/demo/receiver"},
		},
		{
			name: "private http base is accepted in production", mutate: func(m map[string]string) {
				productionDemoEnv(m)
				m["DEMO_RECEIVER_BASE_URL"] = "http://api:8080/demo/receiver"
			},
			wantBase: "http://api:8080/demo/receiver", wantAllowlist: []string{"http://api:8080/demo/receiver"},
		},
		{
			name: "a base path in front is accepted", mutate: func(m map[string]string) { m["DEMO_RECEIVER_BASE_URL"] = "https://hr.example/api/demo/receiver" },
			wantBase: "https://hr.example/api/demo/receiver", wantAllowlist: []string{"https://hr.example/api/demo/receiver"},
		},
		{
			name: "disabled has an empty allowlist and no base url check", mutate: func(m map[string]string) {
				m["DEMO_RECEIVER_ENABLED"] = "false"
				m["DEMO_RECEIVER_BASE_URL"] = "ftp://not-validated/at/all?x#y"
			},
			wantBase: "ftp://not-validated/at/all?x#y", wantAllowlist: nil,
		},
		{
			name: "disabled in production still has an empty allowlist", mutate: func(m map[string]string) {
				productionDemoEnv(m)
				m["DEMO_RECEIVER_ENABLED"] = "false"
			},
			wantBase: "https://api.example.com/demo/receiver", wantAllowlist: nil,
		},
		{
			name: "an empty OUTBOUND_ALLOWLIST is fine", mutate: func(m map[string]string) { m["OUTBOUND_ALLOWLIST"] = "" },
			wantBase: "http://localhost:8080/demo/receiver", wantAllowlist: []string{"http://localhost:8080/demo/receiver"},
		},
		{
			name: "a blank OUTBOUND_ALLOWLIST is fine", mutate: func(m map[string]string) { m["OUTBOUND_ALLOWLIST"] = "   " },
			wantBase: "http://localhost:8080/demo/receiver", wantAllowlist: []string{"http://localhost:8080/demo/receiver"},
		},
		{
			name: "allowlist only with the receiver enabled is fine", mutate: func(m map[string]string) { m["OUTBOUND_ALLOWLIST_ONLY"] = "true" },
			wantBase: "http://localhost:8080/demo/receiver", wantAllowlist: []string{"http://localhost:8080/demo/receiver"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validEnv()
			tt.mutate(m)

			cfg, err := config.Load(env(m))

			if err != nil || cfg.DemoReceiverBaseURL != tt.wantBase || !slices.Equal(cfg.OutboundAllowlist, tt.wantAllowlist) {
				t.Fatalf("base %q, allowlist %v, err %v; want %q and %v", cfg.DemoReceiverBaseURL, cfg.OutboundAllowlist, err, tt.wantBase, tt.wantAllowlist)
			}
		})
	}
}

func TestLoadRejectsAnInvalidDemoReceiverSetup(t *testing.T) {
	setBase := func(base string) func(map[string]string) {
		return func(m map[string]string) { m["DEMO_RECEIVER_BASE_URL"] = base }
	}
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{name: "OUTBOUND_ALLOWLIST is removed", mutate: func(m map[string]string) { m["OUTBOUND_ALLOWLIST"] = "https://x.example/hook" }, wantErr: "ADR 0017"},
		{name: "removed variable is named", mutate: func(m map[string]string) { m["OUTBOUND_ALLOWLIST"] = "x" }, wantErr: "OUTBOUND_ALLOWLIST"},
		{name: "removed variable fails even when the receiver is disabled", mutate: func(m map[string]string) {
			m["DEMO_RECEIVER_ENABLED"] = "false"
			m["OUTBOUND_ALLOWLIST"] = "x"
		}, wantErr: "OUTBOUND_ALLOWLIST"},
		{name: "allowlist only needs the receiver", mutate: func(m map[string]string) {
			m["OUTBOUND_ALLOWLIST_ONLY"] = "true"
			m["DEMO_RECEIVER_ENABLED"] = "false"
		}, wantErr: "OUTBOUND_ALLOWLIST_ONLY"},
		{name: "query", mutate: setBase("https://hr.example/demo/receiver?x=1"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "bare question mark", mutate: setBase("https://hr.example/demo/receiver?"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "fragment", mutate: setBase("https://hr.example/demo/receiver#frag"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "userinfo", mutate: setBase("https://user@hr.example/demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "trailing slash", mutate: setBase("https://hr.example/demo/receiver/"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "dot dot segment", mutate: setBase("https://hr.example/demo/receiver/.."), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "dot dot segment before the suffix", mutate: setBase("https://hr.example/a/../demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "encoded dot dot segment", mutate: setBase("https://hr.example/%2e%2e/demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "dot segment", mutate: setBase("https://hr.example/./demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "backslash segment", mutate: setBase(`https://hr.example/..\demo/receiver`), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "dot dot with a matrix parameter", mutate: setBase("https://hr.example/..;x/demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "other path", mutate: setBase("https://hr.example/other"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "path that merely ends alike", mutate: setBase("https://hr.example/xdemo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "no path", mutate: setBase("https://hr.example"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "ftp scheme", mutate: setBase("ftp://hr.example/demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "no scheme", mutate: setBase("hr.example/demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "no host", mutate: setBase("https:///demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
		{name: "unparsable", mutate: setBase("https://hr.example:port/demo/receiver"), wantErr: "DEMO_RECEIVER_BASE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validEnv()
			tt.mutate(m)

			_, err := config.Load(env(m))

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}
