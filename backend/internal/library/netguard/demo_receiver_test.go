package netguard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/library/netguard"
)

// demoEntry is the allowlist the server derives from DEMO_RECEIVER_BASE_URL (ADR 0017): one entry,
// here on a loopback address, which production would otherwise refuse.
const demoEntry = "http://127.0.0.1:8080/demo/receiver"

func demoOptions() netguard.Options {
	return netguard.Options{Production: true, Allowlist: []string{demoEntry}, LookupTimeout: 50 * time.Millisecond}
}

// demoCases are the destinations US-010 AC3 is about. accepted says whether production lets the
// URL be registered with only the demo receiver on the allowlist.
type demoCase struct {
	name     string
	url      string
	accepted bool
}

var demoCases = []demoCase{
	{name: "the base itself", url: "http://127.0.0.1:8080/demo/receiver", accepted: true},
	{name: "a project receiver", url: "http://127.0.0.1:8080/demo/receiver/proj_X", accepted: true},
	{name: "a project receiver with a query", url: "http://127.0.0.1:8080/demo/receiver/proj_X?fail_first=2", accepted: true},
	{name: "another path on the same host", url: "http://127.0.0.1:8080/v1/projects"},
	{name: "the health path", url: "http://127.0.0.1:8080/healthz"},
	{name: "the database port", url: "http://127.0.0.1:5432/demo/receiver/proj_X"},
	{name: "another port", url: "http://127.0.0.1:8081/demo/receiver/proj_X"},
	{name: "the default port", url: "http://127.0.0.1/demo/receiver/proj_X"},
	{name: "https on the allowlisted port", url: "https://127.0.0.1:8080/demo/receiver/proj_X"},
	{name: "prefix without a segment boundary", url: "http://127.0.0.1:8080/demo/receiver-evil"},
	{name: "dot dot segment", url: "http://127.0.0.1:8080/demo/receiver/../v1"},
	{name: "encoded dot dot segment", url: "http://127.0.0.1:8080/demo/receiver/%2e%2e/v1"},
	{name: "dot dot with a matrix parameter", url: "http://127.0.0.1:8080/demo/receiver/..;x/v1"},
	{name: "backslash traversal", url: `http://127.0.0.1:8080/demo/receiver\..\v1`},
	{name: "encoded backslash traversal", url: "http://127.0.0.1:8080/demo/receiver/..%5c..%5cv1"},
	{name: "another spelling of the same machine", url: "http://localhost:8080/demo/receiver/proj_X"},
	{name: "userinfo", url: "http://user@127.0.0.1:8080/demo/receiver/proj_X"},
}

func TestProductionAcceptsOnlyTheDemoReceiverPrefix(t *testing.T) {
	policy := newPolicy(t, demoOptions())
	for _, tt := range demoCases {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			if (err == nil) != tt.accepted {
				t.Fatalf("%s: accepted = %v, want %v (err %v)", tt.url, err == nil, tt.accepted, err)
			}
		})
	}
}

func TestAllowlistOnlyAcceptsTheDemoReceiverAndNothingPublic(t *testing.T) {
	opts := demoOptions()
	opts.AllowlistOnly = true
	policy := newPolicy(t, opts)
	tests := []struct {
		name     string
		url      string
		accepted bool
	}{
		{name: "the demo receiver", url: "http://127.0.0.1:8080/demo/receiver/proj_X", accepted: true},
		{name: "the demo receiver with a query", url: "http://127.0.0.1:8080/demo/receiver/proj_X?fail_first=2", accepted: true},
		{name: "a public https receiver", url: "https://example.com/hook"},
		{name: "another path on the demo host", url: "http://127.0.0.1:8080/v1/projects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			if (err == nil) != tt.accepted {
				t.Fatalf("%s: accepted = %v, want %v (err %v)", tt.url, err == nil, tt.accepted, err)
			}
		})
	}
}

func TestAnEmptyAllowlistLetsNoInternalDestinationThrough(t *testing.T) {
	policy := newPolicy(t, netguard.Options{Production: true, LookupTimeout: 50 * time.Millisecond})
	tests := []struct{ name, url string }{
		{name: "the demo receiver URL of a disabled deployment", url: "http://127.0.0.1:8080/demo/receiver/proj_X"},
		{name: "https on loopback", url: "https://127.0.0.1:8080/demo/receiver/proj_X"},
		{name: "a private address", url: "https://10.0.0.5/demo/receiver/proj_X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := policy.Check(context.Background(), tt.url); err == nil {
				t.Fatalf("%s must be refused when nothing is allowlisted", tt.url)
			}
		})
	}
}

func TestAllowlistedDialRefusesEverythingButTheDemoReceiverAddress(t *testing.T) {
	dial := newPolicy(t, demoOptions()).AllowlistedDial()
	addresses := []string{"127.0.0.1:5432", "127.0.0.1:8081", "127.0.0.1:80", "localhost:8080", "10.0.0.5:8080"}
	for _, address := range addresses {
		t.Run(address, func(t *testing.T) {
			conn, err := dial(context.Background(), "tcp", address)

			if !errors.Is(err, netguard.ErrDestinationBlocked) || conn != nil {
				t.Fatalf("dial %s: conn %v, err %v; want ErrDestinationBlocked", address, conn, err)
			}
		})
	}
}

// IsAllowlisted picks the client at send time and Check decides at registration, so they must give
// the same answer for every demo case, userinfo included.
func TestIsAllowlistedAgreesWithCheckOnTheDemoCases(t *testing.T) {
	policy := newPolicy(t, demoOptions())
	for _, tt := range demoCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.IsAllowlisted(tt.url); got != tt.accepted {
				t.Fatalf("IsAllowlisted(%s) = %v, Check accepts %v", tt.url, got, tt.accepted)
			}
		})
	}
}
