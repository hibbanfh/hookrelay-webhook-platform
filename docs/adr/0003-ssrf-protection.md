# ADR 0003: SSRF protection with registration-time validation and dial-time IP enforcement

- **Status:** Accepted. Amended 2026-11-09 for US-002: the package is `internal/library/netguard` ([ADR 0012](0012-google-wire-and-per-dependency-libraries.md)), and the registration rules and blocked ranges are extended in [`docs/plans/us-002.md`](../plans/us-002.md) section 3.
- **Date:** 2026-11-08

## Context

Hookrelay makes HTTP requests to URLs that users register. On the public demo that is
server-side request forgery as a feature. Without controls, an attacker could register
`http://169.254.169.254/latest/meta-data/`, `http://localhost:5432`, a Fly private address
(`fdaa::/16`, `*.internal`), or a public hostname whose DNS later flips to a private IP (DNS
rebinding / TOCTOU). The attempt log shows the response body, which would turn a blind SSRF
into a readable one.

Requirements (US-002, US-004, US-010): reject private destinations at registration and at
delivery, in production mode only, with no redirects. A blocked delivery is recorded as
`destination_blocked` and not retried. The demo receiver is the only internal destination
allowed, and it has to be allowlisted explicitly.

## Decision

A single policy in `internal/platform/netguard` is enforced in two places.

**1. Registration time** (`endpoint` domain, through its `URLPolicy` port):
- Syntax (all modes): absolute URL, scheme `http` or `https`, host present, no userinfo
  (`user:pass@`), length <= 2048.
- Production mode: scheme must be `https`; host must not be `localhost` or `*.localhost`,
  `*.internal` or `*.local`. If the host is an IP literal it is checked directly. Otherwise it is
  resolved (2 s timeout) and **every** returned address must be public. Unresolvable hosts are
  rejected (`400`, field `url`).
- URLs matching an entry in `OUTBOUND_ALLOWLIST` (exact scheme + host + port, plus a path
  prefix) skip the production checks. By default the list holds only `DEMO_RECEIVER_BASE_URL`.
- Optional hardening for the public demo: `OUTBOUND_ALLOWLIST_ONLY=true` accepts *only*
  allowlisted URLs. This turns the shared demo key into a closed sandbox (see open questions in
  `docs/architecture.md`).

**2. Delivery time** (the `dispatch` domain's webhook sender adapter). This is the real defense:
- The `http.Transport` gets a custom `DialContext`. It resolves the host itself, rejects the
  connection if **any** resolved IP is blocked, and dials the vetted IP directly. Because the
  checked IP is the dialed IP, DNS rebinding between check and connect is impossible. A
  `net.Dialer.Control` hook re-checks the final socket address as defense in depth.
- `CheckRedirect` returns `http.ErrUseLastResponse` (3xx is recorded as a failed attempt).
- `Proxy: nil` (ignore `HTTP_PROXY` env), so a proxy can't be used to bypass the checks.
- Allowlisted URLs use a second client whose dialer permits only the allowlisted host:port.
  The choice between the two clients is made per request by prefix match on the URL.
- A block surfaces as `netguard.ErrDestinationBlocked`. The sender maps it to
  `AttemptError = destination_blocked`, which the `Delivery` entity treats as terminal.

**Blocked ranges** (using `net/netip`; IPv4-mapped IPv6 is unmapped before checking):
`0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10` (CGNAT), `127.0.0.0/8`, `169.254.0.0/16`,
`172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.168.0.0/16`, `198.18.0.0/15`,
`198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4`, `255.255.255.255/32`,
`::/128`, `::1/128`, `::ffff:0:0:0/96` (IPv4-translated, SIIT), `64:ff9b::/96` (NAT64; can embed private v4), `100::/64`, `2001:db8::/32`,
`fc00::/7` (ULA, includes Fly's `fdaa::/16`), `fe80::/10`, `ff00::/8`.

Development mode skips IP checks, so `http://localhost:9000` and `http://api:8080` work in
docker compose. Redirects stay off in every mode.

## Alternatives considered

| Option | Why not |
|---|---|
| **Validate at registration only** | Defeated by DNS rebinding and by later DNS changes (explicitly in US-004 AC). |
| **Check the hostname resolution, then let `net/http` dial normally** | TOCTOU: the transport resolves again and can get a different answer. The IP that was checked must be the IP that gets dialed. |
| **Egress proxy (e.g. Smokescreen) or network-level egress firewall** | Strongest option and the right one at scale, but it's another service to deploy and Fly.io offers no egress firewall. We keep the `netguard` package boundary so a proxy can be added later. |
| **Allow only ports 80/443** | Reduces the attack surface further, but legitimate receivers sometimes use other ports, and the IP checks already cover internal services. Kept as an easy follow-up. |
| **Allowlist by hostname only** | The allowlist matches scheme + host + port + path prefix, so allowlisting the demo receiver doesn't open the rest of the API's internal surface. |

## Consequences

- Positive: closes metadata, loopback, private-network and rebinding attacks, and is easy to
  explain with one sentence: "the IP we check is the IP we dial".
- Positive: the policy is pure (`netip` prefix tables) and table-test friendly. Dialer tests use
  a fake resolver.
- Negative: production registration needs working DNS, and a receiver whose DNS isn't set up yet
  can't be registered. This is accepted and the error message says why.
- Negative: hosts with mixed public and private records are rejected entirely. That is
  intentionally conservative.
- Follow-up: honoring `Retry-After` (P2) must not reintroduce redirects or proxies.
