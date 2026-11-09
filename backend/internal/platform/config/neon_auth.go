package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// APIRequestTimeout is the per-request deadline of the API. Calls to the identity provider
	// must finish well inside it, which validateNeonAuth enforces.
	APIRequestTimeout = 10 * time.Second

	defaultSessionCookie  = "__Secure-neon-auth.session_token"
	defaultNeonTimeout    = "3s"
	defaultSessionTTL     = "5s"
	maxSessionCacheTTL    = 60 * time.Second
	httpsPrefix           = "https://"
	envNeonBaseURL        = "NEON_AUTH_BASE_URL"
	envDashboardOrigin    = "DASHBOARD_ORIGIN"
	envNeonTimeoutName    = "NEON_AUTH_TIMEOUT"
	envSessionCacheTTLKey = "SESSION_CACHE_TTL"
	envSessionCookieName  = "NEON_AUTH_SESSION_COOKIE"
)

// NeonAuthEnabled reports whether dashboard sessions can be verified. Without a base URL
// (development only) every session is rejected.
func (c Config) NeonAuthEnabled() bool { return c.NeonAuthBaseURL != "" }

// neonAuthSettings are the Neon Auth variables as read from the environment.
type neonAuthSettings struct {
	baseURL, cookie, origin string
	timeout, cacheTTL       time.Duration
}

// neonAuthSettings reads the Neon Auth variables. The base URL has no trailing slash.
func (r *reader) neonAuthSettings() neonAuthSettings {
	return neonAuthSettings{
		baseURL:  strings.TrimRight(r.str(envNeonBaseURL, ""), "/"),
		cookie:   r.str(envSessionCookieName, defaultSessionCookie),
		origin:   r.str(envDashboardOrigin, ""),
		timeout:  r.duration(envNeonTimeoutName, defaultNeonTimeout),
		cacheTTL: r.durationInRange(envSessionCacheTTLKey, defaultSessionTTL, 0, maxSessionCacheTTL),
	}
}

// durationInRange parses a Go duration that must satisfy min <= d <= max.
func (r *reader) durationInRange(name, def string, minimum, maximum time.Duration) time.Duration {
	v := r.str(name, def)
	d, err := time.ParseDuration(v)
	if err != nil || d < minimum || d > maximum {
		r.fail(name, "must be a Go duration between %s and %s, got %q", minimum, maximum, v)
	}
	return d
}

// validateNeonAuth checks the rules that span the Neon Auth variables and the environment.
func validateNeonAuth(c Config) error {
	var errs []error
	if c.NeonAuthTimeout >= APIRequestTimeout {
		errs = append(errs, fmt.Errorf("%s must be shorter than the %s request timeout", envNeonTimeoutName, APIRequestTimeout))
	}
	if c.IsProduction() && c.NeonAuthBaseURL == "" {
		errs = append(errs, fmt.Errorf("%s is required in production", envNeonBaseURL))
	}
	if c.NeonAuthBaseURL == "" {
		return errors.Join(errs...)
	}
	errs = append(errs, validateNeonBaseURL(c), validateDashboardOrigin(c))
	return errors.Join(errs...)
}

func validateNeonBaseURL(c Config) error {
	if c.IsProduction() && !strings.HasPrefix(c.NeonAuthBaseURL, httpsPrefix) {
		return fmt.Errorf("%s must start with https:// in production", envNeonBaseURL)
	}
	u, err := url.Parse(c.NeonAuthBaseURL)
	if err != nil || u.Host == "" || !isAllowedScheme(u.Scheme) {
		return fmt.Errorf("%s must be an absolute http(s) URL", envNeonBaseURL)
	}
	return nil
}

// validateDashboardOrigin accepts scheme://host[:port] only, the exact form of an Origin header.
// Production requires https: the session cookie is Secure and must only travel over TLS.
func validateDashboardOrigin(c Config) error {
	origin := c.DashboardOrigin
	if origin == "" {
		return fmt.Errorf("%s is required when %s is set", envDashboardOrigin, envNeonBaseURL)
	}
	if c.IsProduction() && !strings.HasPrefix(origin, httpsPrefix) {
		return fmt.Errorf("%s must start with https:// in production", envDashboardOrigin)
	}
	u, err := url.Parse(origin)
	if err != nil || !isBareOrigin(u) {
		return fmt.Errorf("%s must be scheme://host[:port] with no path, got %q", envDashboardOrigin, origin)
	}
	return nil
}

func isAllowedScheme(scheme string) bool {
	return scheme == "http" || scheme == "https"
}

// isBareOrigin reports whether u is exactly scheme://host[:port].
func isBareOrigin(u *url.URL) bool {
	hasExtras := u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil
	return u.Host != "" && isAllowedScheme(u.Scheme) && !hasExtras
}
