package netguard

import "net/netip"

// blockedCIDRs are the destinations a production webhook may never target (ADR 0003 and
// docs/plans/us-002.md section 3). The IPv6 transition ranges are listed because each of
// them can embed or reach a private IPv4 address.
var blockedCIDRs = [...]string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",   // CGNAT
	"127.0.0.0/8",     // loopback
	"169.254.0.0/16",  // link-local, includes the cloud metadata address
	"172.16.0.0/12",   // RFC 1918
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // documentation
	"192.88.99.0/24",  // 6to4 relay anycast
	"192.168.0.0/16",  // RFC 1918
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // documentation
	"203.0.113.0/24",  // documentation
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved
	"255.255.255.255/32",
	"::/128",
	"::1/128",
	"::ffff:0:0:0/96", // IPv4-translated (SIIT), embeds an IPv4
	"::/96",           // IPv4-compatible
	"64:ff9b::/96",    // NAT64, can embed a private IPv4
	"64:ff9b:1::/48",  // local-use NAT64
	"100::/64",        // discard-only
	"2001::/32",       // Teredo
	"2001:db8::/32",   // documentation
	"2002::/16",       // 6to4, embeds an IPv4
	"fc00::/7",        // unique local, includes Fly's fdaa::/16
	"fe80::/10",       // link-local
	"fec0::/10",       // site-local (deprecated)
	"ff00::/8",        // multicast
}

func parseBlockedRanges() []netip.Prefix {
	out := make([]netip.Prefix, 0, len(blockedCIDRs))
	for _, cidr := range blockedCIDRs {
		out = append(out, netip.MustParsePrefix(cidr))
	}
	return out
}
