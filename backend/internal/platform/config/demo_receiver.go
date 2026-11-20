package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	envDemoBaseURL   = "DEMO_RECEIVER_BASE_URL"
	envAllowlistOnly = "OUTBOUND_ALLOWLIST_ONLY"
	demoReceiverPath = "/demo/receiver"

	// envRemovedAllowlist was a free-form list of SSRF-exempt destinations. Setting it is a startup
	// error, so an old deployment cannot silently lose an entry.
	envRemovedAllowlist    = "OUTBOUND_ALLOWLIST"
	removedAllowlistReason = "removed in US-010: the only allowlisted destination is DEMO_RECEIVER_BASE_URL (ADR 0017)"
)

// DemoReceiverURL is the URL of the demo receiver of one project: the base URL and the project ID.
// The project response shows it as demo_receiver_url, the seed registers it, and the receiver
// matches endpoints against it, so every caller builds it here. A trailing slash on the base is
// not doubled (the validated base has none; the default is trimmed the same way).
func DemoReceiverURL(baseURL, projectID string) string {
	return strings.TrimRight(baseURL, "/") + "/" + projectID
}

// demoAllowlist is the SSRF allowlist: exactly the demo receiver base URL when the receiver is on,
// and nothing otherwise (ADR 0017 section 4). There is no other way to extend it.
func demoAllowlist(enabled bool, baseURL string) []string {
	if !enabled {
		return nil
	}
	return []string{baseURL}
}

// validateDemoReceiver checks the receiver settings that span several variables. The base URL is
// only checked when the receiver is on: a disabled receiver never uses it.
func validateDemoReceiver(c Config) error {
	var errs []error
	if c.OutboundAllowlistOnly && !c.DemoReceiverEnabled {
		errs = append(errs, fmt.Errorf("%s=true needs DEMO_RECEIVER_ENABLED=true: no destination could be registered otherwise", envAllowlistOnly))
	}
	if c.DemoReceiverEnabled {
		errs = append(errs, validateDemoBaseURL(c.DemoReceiverBaseURL))
	}
	return errors.Join(errs...)
}

// validateDemoBaseURL accepts an absolute http(s) URL with a host and no userinfo, query,
// fragment or dot segments, whose path ends with /demo/receiver without a trailing slash. That
// path is the only place the route exists, so any other base is a configuration error instead of
// a 404 found later. http is allowed in production on purpose: the receiver may be reached over
// a private network.
func validateDemoBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", envDemoBaseURL, err)
	}
	if !isAllowedScheme(u.Scheme) || u.Hostname() == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL with a host, got %q", envDemoBaseURL, raw)
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("%s must not contain credentials, a query or a fragment", envDemoBaseURL)
	}
	if hasDotSegment(u.Path) {
		return fmt.Errorf("%s must not contain . or .. path segments", envDemoBaseURL)
	}
	if !strings.HasSuffix(u.Path, demoReceiverPath) {
		return fmt.Errorf("%s must end with %s and have no trailing slash", envDemoBaseURL, demoReceiverPath)
	}
	return nil
}

// hasDotSegment reports a . or .. segment, also after percent-decoding (url.Parse decoded the
// path), with a backslash read as a slash and with a ";parameter" suffix dropped, the way
// library/netguard reads it.
func hasDotSegment(path string) bool {
	for _, segment := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
		name, _, _ := strings.Cut(segment, ";")
		if name == "." || name == ".." {
			return true
		}
	}
	return false
}
