package netguard_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/library/netguard"
)

// fakeResolver answers from a table. A host mapped to a nil slice returns an empty answer,
// a host in errs returns that error, and a host in hang blocks until the context ends.
type fakeResolver struct {
	answers map[string][]netip.Addr
	errs    map[string]error
	hang    map[string]bool
}

func (f fakeResolver) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	if f.hang[host] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := f.errs[host]; err != nil {
		return nil, err
	}
	return f.answers[host], nil
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, a := range s {
		out = append(out, netip.MustParseAddr(a))
	}
	return out
}

func testResolver() fakeResolver {
	return fakeResolver{
		answers: map[string][]netip.Addr{
			"example.com":     addrs("93.184.216.34"),
			"dual.example":    addrs("93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"),
			"internal.corp":   addrs("10.1.2.3"),
			"mixed.example":   addrs("93.184.216.34", "10.0.0.5"),
			"mapped.example":  addrs("::ffff:127.0.0.1"),
			"metadata.proxy":  addrs("169.254.169.254"),
			"ula.example":     addrs("fd12:3456::1"),
			"empty.example":   nil,
			"public.v4.mixed": addrs("8.8.8.8"),
		},
		errs: map[string]error{
			"nx.example":       errors.New("no such host"),
			"servfail.example": errors.New("server misbehaving"),
		},
		hang: map[string]bool{"slow.example": true},
	}
}

func newPolicy(t *testing.T, o netguard.Options) *netguard.Policy {
	t.Helper()
	if o.Resolver == nil {
		o.Resolver = testResolver()
	}
	policy, err := netguard.New(o)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func production() netguard.Options {
	return netguard.Options{Production: true, LookupTimeout: 50 * time.Millisecond}
}

func TestProductionRejectsNonPublicDestinations(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"http scheme", "http://example.com/hook"},
		{"localhost", "https://localhost/hook"},
		{"localhost with trailing dot", "https://localhost./hook"},
		{"localhost mixed case", "https://LocalHost/hook"},
		{"subdomain of localhost", "https://api.localhost/hook"},
		{"internal suffix", "https://db.internal/hook"},
		{"local suffix", "https://printer.local/hook"},
		{"IPv4 loopback", "https://127.0.0.1/hook"},
		{"IPv4 loopback range", "https://127.8.9.10/hook"},
		{"IPv4 unspecified", "https://0.0.0.0/hook"},
		{"RFC 1918 10/8", "https://10.0.0.1/hook"},
		{"RFC 1918 172.16/12", "https://172.16.0.1/hook"},
		{"RFC 1918 172.31", "https://172.31.255.254/hook"},
		{"RFC 1918 192.168/16", "https://192.168.1.1/hook"},
		{"link-local", "https://169.254.10.10/hook"},
		{"cloud metadata", "https://169.254.169.254/latest/meta-data/"},
		{"CGNAT", "https://100.64.0.1/hook"},
		{"benchmark range", "https://198.18.0.1/hook"},
		{"multicast", "https://224.0.0.1/hook"},
		{"reserved", "https://240.0.0.1/hook"},
		{"6to4 relay anycast", "https://192.88.99.1/hook"},
		{"IPv6 loopback", "https://[::1]/hook"},
		{"IPv6 unspecified", "https://[::]/hook"},
		{"IPv6 unique local", "https://[fd12:3456:789a::1]/hook"},
		{"IPv6 Fly private network", "https://[fdaa:0:1::3]/hook"},
		{"IPv6 link-local", "https://[fe80::1]/hook"},
		{"IPv6 site-local", "https://[fec0::1]/hook"},
		{"IPv6 multicast", "https://[ff02::1]/hook"},
		{"IPv6 documentation", "https://[2001:db8::1]/hook"},
		{"IPv4-mapped loopback", "https://[::ffff:127.0.0.1]/hook"},
		{"IPv4-mapped loopback in hex", "https://[::ffff:7f00:1]/hook"},
		{"IPv4-mapped metadata", "https://[::ffff:169.254.169.254]/hook"},
		{"IPv4-mapped RFC 1918", "https://[::ffff:10.0.0.1]/hook"},
		{"IPv4-compatible loopback", "https://[::127.0.0.1]/hook"},
		{"IPv4-translated (SIIT) loopback in hex", "https://[::ffff:0:7f00:1]/hook"},
		{"IPv4-translated (SIIT) loopback dotted", "https://[::ffff:0:127.0.0.1]/hook"},
		{"6to4 embedding a private address", "https://[2002:7f00:1::1]/hook"},
		{"Teredo", "https://[2001:0:4136:e378:8000:63bf:3fff:fdd2]/hook"},
		{"local NAT64", "https://[64:ff9b:1::a00:1]/hook"},
		{"NAT64 embedding loopback", "https://[64:ff9b::7f00:1]/hook"},
		{"IPv6 with a zone", "https://[fe80::1%25eth0]/hook"},
		{"decimal IPv4", "https://2130706433/hook"},
		{"hex IPv4", "https://0x7f.0.0.1/hook"},
		{"short IPv4", "https://127.1/hook"},
		{"octal IPv4", "https://0177.0.0.1/hook"},
		{"non-ASCII host", "https://exämple.com/hook"},
		{"hostname resolving to RFC 1918", "https://internal.corp/hook"},
		{"hostname resolving to link-local metadata", "https://metadata.proxy/hook"},
		{"hostname resolving to IPv4-mapped loopback", "https://mapped.example/hook"},
		{"hostname resolving to IPv6 ULA", "https://ula.example/hook"},
		{"one private address among public ones", "https://mixed.example/hook"},
	}
	policy := newPolicy(t, production())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			if err == nil {
				t.Fatalf("%s must be rejected", tt.url)
			}
		})
	}
}

func TestProductionAcceptsPublicDestinations(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"public hostname", "https://example.com/hook"},
		{"hostname with a port and query", "https://example.com:8443/hook?x=1"},
		{"hostname with both address families", "https://dual.example/hook"},
		{"trailing dot hostname", "https://example.com./hook"},
		{"public IPv4 literal", "https://93.184.216.34/hook"},
		{"public IPv6 literal", "https://[2606:2800:220:1:248:1893:25c8:1946]/hook"},
		{"just outside 172.16/12", "https://172.32.0.1/hook"},
		{"just outside 100.64/10", "https://100.128.0.1/hook"},
		{"hostname with a public name ending in internal-ish text", "https://example.com/internal"},
	}
	policy := newPolicy(t, production())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			if err != nil {
				t.Fatalf("%s: %v", tt.url, err)
			}
		})
	}
}

func TestDNSFailuresFailClosedWithoutLeakingAnswers(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"NXDOMAIN", "https://nx.example/hook"},
		{"SERVFAIL", "https://servfail.example/hook"},
		{"empty answer", "https://empty.example/hook"},
		{"timeout", "https://slow.example/hook"},
		{"unknown host", "https://unknown.example/hook"},
	}
	policy := newPolicy(t, production())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			if err == nil || err.Error() != "host could not be resolved" {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRejectionNeverContainsAResolvedAddress(t *testing.T) {
	policy := newPolicy(t, production())

	err := policy.Check(context.Background(), "https://internal.corp/hook")

	if err == nil || strings.Contains(err.Error(), "10.1.2.3") || strings.Contains(err.Error(), "10.") {
		t.Fatalf("error must not echo DNS answers: %v", err)
	}
}

func TestAllowlist(t *testing.T) {
	opts := production()
	opts.Allowlist = []string{"http://localhost:8080/demo/receiver", "https://hooks.partner.example/in"}
	tests := []struct {
		name    string
		url     string
		allowed bool
	}{
		{"exact entry", "http://localhost:8080/demo/receiver", true},
		{"path below the prefix", "http://localhost:8080/demo/receiver/proj_1", true},
		{"query is allowed", "http://localhost:8080/demo/receiver/proj_1?x=1", true},
		{"prefix must end on a segment boundary", "http://localhost:8080/demo/receiver-evil", false},
		{"another path", "http://localhost:8080/v1/endpoints", false},
		{"another port", "http://localhost:9090/demo/receiver/x", false},
		{"another scheme", "https://localhost:8080/demo/receiver/x", false},
		{"another host", "http://127.0.0.1:8080/demo/receiver/x", false},
		{"dot segments never match", "http://localhost:8080/demo/receiver/../../v1/endpoints", false},
		{"encoded dot segments never match", "http://localhost:8080/demo/receiver/%2e%2e/%2e%2e/v1", false},
		{"mixed-case encoded dot segments", "http://localhost:8080/demo/receiver/%2E%2e/x", false},
		{"encoded traversal in one segment", "http://localhost:8080/demo/receiver/..%2f..%2fv1", false},
		{"backslash traversal", "http://localhost:8080/demo/receiver/..%5c..%5cv1", false},
		{"case-insensitive host", "http://LOCALHOST:8080/demo/receiver/x", true},
		{"second entry matches", "https://hooks.partner.example/in/abc", true},
		{"default port is implied", "https://hooks.partner.example:443/in/abc", true},
	}
	policy := newPolicy(t, opts)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			// An allowlisted URL passes. A URL that does not match falls through to the
			// production rules: every non-matching URL here is http or localhost, so it fails
			// except the partner host, which is public and would also pass on its own.
			if (err == nil) != tt.allowed {
				t.Fatalf("%s: allowed=%v, err=%v", tt.url, tt.allowed, err)
			}
		})
	}
}

func TestAllowlistOnlyRejectsEverythingElse(t *testing.T) {
	opts := production()
	opts.Allowlist = []string{"https://example.com/hook"}
	opts.AllowlistOnly = true
	tests := []struct {
		name    string
		url     string
		allowed bool
	}{
		{"allowlisted", "https://example.com/hook/a", true},
		{"public but not allowlisted", "https://dual.example/hook", false},
	}
	policy := newPolicy(t, opts)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(context.Background(), tt.url)

			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v, err=%v", tt.allowed, err)
			}
		})
	}
}

func TestDevelopmentModeChecksNothing(t *testing.T) {
	tests := []struct{ name, url string }{
		{"localhost", "http://localhost:9000/hook"},
		{"private address", "http://10.0.0.1/hook"},
		{"compose service name", "http://api:8080/hook"},
	}
	policy := newPolicy(t, netguard.Options{Production: false})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := policy.Check(context.Background(), tt.url); err != nil {
				t.Fatalf("%s: %v", tt.url, err)
			}
		})
	}
}

func TestNewRejectsAnUnusableAllowlistEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"no scheme", "localhost:8080/demo"},
		{"no host", "https:///path"},
		{"unsupported scheme", "ftp://example.com/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := netguard.New(netguard.Options{Production: true, Allowlist: []string{tt.entry}, Resolver: testResolver()})

			if err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// With AllowlistOnly a URL that does not match the allowlist is rejected whatever its host is,
// so these cases prove the dot-segment rule itself and not the production rules behind it.
func TestAllowlistOnlyDoesNotMatchDotSegments(t *testing.T) {
	opts := production()
	opts.Allowlist = []string{"https://example.com/in"}
	opts.AllowlistOnly = true
	tests := []struct{ name, url string }{
		{"dot dot", "https://example.com/in/../admin"},
		{"single dot", "https://example.com/in/./x"},
		{"encoded dot dot", "https://example.com/in/%2e%2e/admin"},
		{"encoded slash", "https://example.com/in/..%2fadmin"},
		{"backslash", "https://example.com/in/..%5cadmin"},
		{"matrix parameter on dot dot", "https://example.com/in/..;x/admin"},
		{"matrix parameter on a dot", "https://example.com/in/.;/admin"},
	}
	policy := newPolicy(t, opts)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := policy.Check(context.Background(), tt.url); err == nil {
				t.Fatalf("%s must not match the allowlist", tt.url)
			}
		})
	}
}
