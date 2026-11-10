// Package netguard decides whether a webhook destination may be registered (SSRF
// protection, ADR 0003). Registration only resolves DNS and never calls the URL. The dial-time
// guard that makes "the IP we check is the IP we dial" true arrives with delivery (US-004).
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/google/wire"
)

// ProviderSet provides the registration policy.
var ProviderSet = wire.NewSet(New)

const defaultLookupTimeout = 2 * time.Second

// Resolver looks up addresses. *net.Resolver satisfies it; tests inject a fake.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Options configure the policy. Without Production the policy checks nothing, so development
// can use localhost and compose service names.
type Options struct {
	Production    bool
	Allowlist     []string
	AllowlistOnly bool
	LookupTimeout time.Duration
	Resolver      Resolver
}

// Rejection is the reason a URL was refused. Its text is safe to show to API clients and never
// contains a resolved address.
type Rejection struct{ Reason string }

// Error implements error.
func (r *Rejection) Error() string { return r.Reason }

func reject(reason string) error { return &Rejection{Reason: reason} }

// Policy applies the registration rules.
type Policy struct {
	production    bool
	allowlist     []allowEntry
	allowlistOnly bool
	timeout       time.Duration
	resolver      Resolver
	blocked       []netip.Prefix
}

// New builds a Policy. It fails when an allowlist entry is not an absolute http(s) URL.
func New(o Options) (*Policy, error) {
	entries, err := parseAllowlist(o.Allowlist)
	if err != nil {
		return nil, err
	}
	p := &Policy{
		production:    o.Production,
		allowlist:     entries,
		allowlistOnly: o.AllowlistOnly,
		timeout:       o.LookupTimeout,
		resolver:      o.Resolver,
		blocked:       parseBlockedRanges(),
	}
	if p.timeout <= 0 {
		p.timeout = defaultLookupTimeout
	}
	if p.resolver == nil {
		p.resolver = net.DefaultResolver
	}
	return p, nil
}

// Check returns nil when rawURL may be registered. Any error means the URL is rejected.
func (p *Policy) Check(ctx context.Context, rawURL string) error {
	if !p.production {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil {
		return reject("must be an absolute URL without credentials")
	}
	if p.matchesAllowlist(u) {
		return nil
	}
	if p.allowlistOnly {
		return reject("is not on the allowed destinations list")
	}
	if u.Scheme != "https" {
		return reject("must use https")
	}
	return p.checkHost(ctx, u.Hostname())
}

func (p *Policy) checkHost(ctx context.Context, rawHost string) error {
	host, ok := normalizeHost(rawHost)
	if !ok {
		return reject("host must be ASCII; send internationalized names as punycode")
	}
	if isDeniedName(host) {
		return reject("host is not allowed")
	}
	addr, parseErr := netip.ParseAddr(host)
	if parseErr == nil {
		return p.checkAddr(addr)
	}
	if looksNumeric(host) {
		return reject("host is not a valid name or address")
	}
	return p.checkResolved(ctx, host)
}

func (p *Policy) checkResolved(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	addrs, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return reject("host could not be resolved")
	}
	for _, addr := range addrs {
		if err := p.checkAddr(addr); err != nil {
			return err
		}
	}
	return nil
}

// checkAddr rejects zones and anything in a blocked range. IPv4-mapped IPv6 is judged as IPv4.
func (p *Policy) checkAddr(addr netip.Addr) error {
	if addr.Zone() != "" {
		return reject("host must not carry an address zone")
	}
	addr = addr.Unmap()
	if !addr.IsValid() || p.isBlocked(addr) {
		return reject("host must be a public address")
	}
	return nil
}

func (p *Policy) isBlocked(addr netip.Addr) bool {
	isSpecial := addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() || addr.IsUnspecified()
	if isSpecial {
		return true
	}
	for _, prefix := range p.blocked {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// normalizeHost lowercases the host and strips one trailing dot, so "localhost." cannot
// bypass the name rules. Non-ASCII hosts are refused.
func normalizeHost(host string) (string, bool) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return "", false
	}
	for _, c := range []byte(host) {
		if c >= 0x80 {
			return "", false
		}
	}
	return host, true
}

var deniedSuffixes = [...]string{".localhost", ".internal", ".local"}

func isDeniedName(host string) bool {
	if host == "localhost" || host == "internal" || host == "local" {
		return true
	}
	for _, suffix := range deniedSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// looksNumeric catches 2130706433, 0x7f.0.0.1 and 127.1, which some resolvers read as loopback
// although they are not valid IP literals.
func looksNumeric(host string) bool {
	last := host[strings.LastIndex(host, ".")+1:]
	return last != "" && (isAllDigits(last) || strings.HasPrefix(last, "0x"))
}

func isAllDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var errAllowlistEntry = errors.New("allowlist entry must be an absolute http or https URL")

func parseAllowlist(raw []string) ([]allowEntry, error) {
	entries := make([]allowEntry, 0, len(raw))
	for _, r := range raw {
		entry, err := parseAllowEntry(r)
		if err != nil {
			return nil, fmt.Errorf("allowlist entry %q: %w", r, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
