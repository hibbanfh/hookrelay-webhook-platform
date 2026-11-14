package netguard_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/library/netguard"
)

// countingListener accepts connections and counts them, so a test can prove that no socket was opened.
type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func listen(t *testing.T) *countingListener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cl := &countingListener{Listener: l}
	t.Cleanup(func() { _ = l.Close() }) //nolint:errcheck // closing a test listener
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			cl.accepted.Add(1)
			_ = conn.Close() //nolint:errcheck // the test only counts connections
		}
	}()
	return cl
}

func (c *countingListener) port() string {
	return strconv.Itoa(c.Addr().(*net.TCPAddr).Port)
}

func productionPolicy(t *testing.T, r netguard.Resolver) *netguard.Policy {
	t.Helper()
	return newPolicy(t, netguard.Options{Production: true, Resolver: r})
}

func TestGuardedDialBlocksWithoutOpeningASocket(t *testing.T) {
	l := listen(t)
	resolver := testResolver()
	resolver.answers["rebind.example"] = addrs("127.0.0.1")
	tests := []struct {
		name string
		host string
	}{
		{"a name that now resolves to loopback", "rebind.example"},
		{"the metadata address", "169.254.169.254"},
		{"an IPv4-mapped private address", "::ffff:10.0.0.1"},
		{"a name with a public and a private address", "mixed.example"},
		{"a name that resolves to a mapped loopback", "mapped.example"},
		{"a unique local IPv6 name", "ula.example"},
		{"a loopback literal", "127.0.0.1"},
	}
	dial := productionPolicy(t, resolver).GuardedDial()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := dial(context.Background(), "tcp", net.JoinHostPort(tt.host, l.port()))

			if conn != nil {
				_ = conn.Close() //nolint:errcheck // the assertion below fails the test
			}
			if !errors.Is(err, netguard.ErrDestinationBlocked) {
				t.Fatalf("got %v, want ErrDestinationBlocked", err)
			}
		})
	}
	time.Sleep(50 * time.Millisecond) // let a stray connection show up in the counter
	if n := l.accepted.Load(); n != 0 {
		t.Fatalf("the listener accepted %d connections, want 0", n)
	}
}

type dnsFailureResolver struct{}

func (dnsFailureResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestGuardedDialKeepsTheDNSErrorForTheCaller(t *testing.T) {
	dial := productionPolicy(t, dnsFailureResolver{}).GuardedDial()

	_, err := dial(context.Background(), "tcp", "nx.example:443")

	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || errors.Is(err, netguard.ErrDestinationBlocked) {
		t.Fatalf("got %v, want a wrapped *net.DNSError that is not a block", err)
	}
}

func TestGuardedDialTreatsAnEmptyAnswerAsADNSError(t *testing.T) {
	dial := productionPolicy(t, testResolver()).GuardedDial()

	_, err := dial(context.Background(), "tcp", "empty.example:443")

	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("got %v, want a *net.DNSError", err)
	}
}

func TestGuardedDialInDevelopmentAllowsLoopback(t *testing.T) {
	l := listen(t)
	dial := newPolicy(t, netguard.Options{Production: false}).GuardedDial()

	conn, err := dial(context.Background(), "tcp", "127.0.0.1:"+l.port())

	if err != nil {
		t.Fatalf("development must dial loopback: %v", err)
	}
	_ = conn.Close() //nolint:errcheck // closing the client side of a test connection
}

func TestAllowlistedDialConnectsOnlyToTheAllowlistedHostAndPort(t *testing.T) {
	allowed, other := listen(t), listen(t)
	p := newPolicy(t, netguard.Options{Production: true, Allowlist: []string{"http://127.0.0.1:" + allowed.port() + "/demo"}})
	dial := p.AllowlistedDial()

	conn, okErr := dial(context.Background(), "tcp", "127.0.0.1:"+allowed.port())
	_, otherPortErr := dial(context.Background(), "tcp", "127.0.0.1:"+other.port())
	_, otherHostErr := dial(context.Background(), "tcp", "localhost:"+allowed.port())

	if okErr != nil {
		t.Fatalf("the allowlisted address must connect: %v", okErr)
	}
	_ = conn.Close() //nolint:errcheck // closing the client side of a test connection
	if !errors.Is(otherPortErr, netguard.ErrDestinationBlocked) || !errors.Is(otherHostErr, netguard.ErrDestinationBlocked) {
		t.Fatalf("other port: %v, other host: %v", otherPortErr, otherHostErr)
	}
	if other.accepted.Load() != 0 {
		t.Fatal("no connection may reach a port outside the allowlist")
	}
}

func TestIsAllowlisted(t *testing.T) {
	p := newPolicy(t, netguard.Options{Allowlist: []string{"http://api:8080/demo/receiver"}})
	tests := []struct {
		url  string
		want bool
	}{
		{"http://api:8080/demo/receiver", true},
		{"http://api:8080/demo/receiver/abc", true},
		{"http://api:8080/demo/receiver-evil", false},
		{"http://api:8080/demo/receiver/../admin", false},
		{"https://api:8080/demo/receiver", false},
		{"http://api:9090/demo/receiver", false},
		{"http://evil/demo/receiver", false},
		{"://bad", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := p.IsAllowlisted(tt.url); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
