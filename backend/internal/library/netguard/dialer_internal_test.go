package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

// recordingConnect remembers the addresses the dialer was asked to open and fails each one.
type recordingConnect struct{ addresses []string }

func (r *recordingConnect) dial(_ context.Context, _, address string) (net.Conn, error) {
	r.addresses = append(r.addresses, address)
	return nil, errors.New("refused")
}

func TestGuardedDialDialsTheAddressItChecked(t *testing.T) {
	p, err := New(Options{Production: true, Resolver: staticResolver{"example.com": {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("2606:2800:220:1::1")}}})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recordingConnect{}
	p.connect = rec.dial

	_, dialErr := p.GuardedDial()(context.Background(), "tcp", "example.com:443")

	want := []string{"93.184.216.34:443", "[2606:2800:220:1::1]:443"}
	if dialErr == nil || len(rec.addresses) != 2 || rec.addresses[0] != want[0] || rec.addresses[1] != want[1] {
		t.Fatalf("dialed %v (err %v), want %v in order", rec.addresses, dialErr, want)
	}
}

func TestControlCheckRefusesABlockedSocketAddress(t *testing.T) {
	p, err := New(Options{Production: true})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		address string
		blocked bool
	}{
		{"127.0.0.1:80", true},
		{"[::1]:80", true},
		{"10.1.2.3:443", true},
		{"[::ffff:192.168.0.1]:80", true},
		{"not-an-address", true},
		{"93.184.216.34:443", false},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			err := p.controlCheck("tcp", tt.address, nil)

			if tt.blocked != errors.Is(err, ErrDestinationBlocked) {
				t.Fatalf("got %v, blocked want %v", err, tt.blocked)
			}
		})
	}
}
