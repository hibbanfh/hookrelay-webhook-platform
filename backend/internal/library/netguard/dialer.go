package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"syscall"
)

// ErrDestinationBlocked means a connection was refused because the destination is not allowed:
// a blocked address in production, or a host and port outside the allowlist.
var ErrDestinationBlocked = errors.New("netguard: destination blocked")

// DialFunc opens a connection. It has the signature of http.Transport.DialContext.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// GuardedDial returns the dialer for ordinary destinations. In development it is a plain dialer,
// so localhost and compose service names work. In production it resolves the host, refuses the
// connection when any address is blocked, and dials only the addresses it checked (ADR 0003:
// the IP we check is the IP we dial), so a DNS answer that changes after registration cannot
// reach a private network.
func (p *Policy) GuardedDial() DialFunc {
	if !p.production {
		return (&net.Dialer{}).DialContext
	}
	return p.dialChecked
}

// AllowlistedDial returns the dialer for destinations on the allowlist. It connects only to a
// host and port that an allowlist entry names, without address checks: the demo receiver may
// live on a private address such as api:8080. Anything else is blocked.
func (p *Policy) AllowlistedDial() DialFunc {
	dialer := &net.Dialer{}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if !p.allowsHostPort(address) {
			return nil, fmt.Errorf("dial %s: %w", address, ErrDestinationBlocked)
		}
		return dialer.DialContext(ctx, network, address)
	}
}

// IsAllowlisted reports whether rawURL matches an allowlist entry (scheme, host, port, path prefix).
func (p *Policy) IsAllowlisted(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return p.matchesAllowlist(u)
}

func (p *Policy) dialChecked(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split address %q: %w", address, err)
	}
	addrs, err := p.resolveAll(ctx, host)
	if err != nil {
		return nil, err
	}
	if err := p.checkAll(addrs); err != nil {
		return nil, fmt.Errorf("dial %s: %w", host, ErrDestinationBlocked)
	}
	return p.dialVetted(ctx, network, addrs, port)
}

// resolveAll returns the IP literal itself, or every address the resolver knows for the name.
// A resolver error stays wrapped so a *net.DNSError survives for the caller to classify.
func (p *Policy) resolveAll(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	addrs, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: %w", host, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true})
	}
	return addrs, nil
}

// checkAll fails when any address is blocked: a name that also points at a private address is
// not safe, whatever order the dialer would try them in.
func (p *Policy) checkAll(addrs []netip.Addr) error {
	for _, addr := range addrs {
		if err := p.checkAddr(addr); err != nil {
			return err
		}
	}
	return nil
}

// dialVetted tries the checked addresses in order until one connects.
func (p *Policy) dialVetted(ctx context.Context, network string, addrs []netip.Addr, port string) (net.Conn, error) {
	var lastErr error
	for _, addr := range addrs {
		conn, err := p.connect(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// controlCheck runs on the socket just before connect, with the address the kernel is about to
// use. It repeats the check as defense in depth.
func (p *Policy) controlCheck(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("control %q: %w", address, ErrDestinationBlocked)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || p.checkAddr(addr) != nil {
		return fmt.Errorf("control %s: %w", host, ErrDestinationBlocked)
	}
	return nil
}

func (p *Policy) allowsHostPort(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	name, ok := normalizeHost(host)
	if !ok {
		return false
	}
	for _, e := range p.allowlist {
		if e.host == name && e.port == port {
			return true
		}
	}
	return false
}
