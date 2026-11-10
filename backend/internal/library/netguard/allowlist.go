package netguard

import (
	"net/url"
	"strings"
)

// allowEntry is one OUTBOUND_ALLOWLIST item: exact scheme, host and port, and a path prefix.
type allowEntry struct {
	scheme string
	host   string
	port   string
	path   string
}

var defaultPorts = map[string]string{"http": "80", "https": "443"}

func parseAllowEntry(raw string) (allowEntry, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return allowEntry{}, errAllowlistEntry
	}
	host, ok := normalizeHost(u.Hostname())
	if _, known := defaultPorts[u.Scheme]; !known || !ok {
		return allowEntry{}, errAllowlistEntry
	}
	return allowEntry{
		scheme: u.Scheme,
		host:   host,
		port:   portOf(u),
		path:   strings.TrimRight(u.Path, "/"),
	}, nil
}

func portOf(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	return defaultPorts[u.Scheme]
}

func (p *Policy) matchesAllowlist(u *url.URL) bool {
	host, ok := normalizeHost(u.Hostname())
	if !ok || hasDotSegment(u.Path) {
		return false
	}
	for _, e := range p.allowlist {
		if e.matches(u, host) {
			return true
		}
	}
	return false
}

// matches compares scheme, host and port exactly. The path must equal the prefix or continue
// it after a "/", so /demo/receiver does not match /demo/receiver-evil.
func (e allowEntry) matches(u *url.URL, host string) bool {
	sameOrigin := e.scheme == u.Scheme && e.host == host && e.port == portOf(u)
	if !sameOrigin {
		return false
	}
	return e.path == "" || u.Path == e.path || strings.HasPrefix(u.Path, e.path+"/")
}

// hasDotSegment reports "." or ".." path segments. u.Path is already percent-decoded, so
// %2e%2e and ..%2f are caught; a backslash counts as a separator because some servers treat it as one.
func hasDotSegment(decodedPath string) bool {
	normalized := strings.ReplaceAll(decodedPath, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		// Some servers drop a ";parameter" suffix, so "..;x" is a dot segment to them.
		name, _, _ := strings.Cut(segment, ";")
		if name == "." || name == ".." {
			return true
		}
	}
	return false
}
