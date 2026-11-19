package entity_test

import (
	"testing"

	"hookrelay/internal/dispatch/domain/entity"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		in     string
		want   entity.Status
		wantOK bool
	}{
		{"pending", entity.StatusPending, true},
		{"succeeded", entity.StatusSucceeded, true},
		{"failed", entity.StatusFailed, true},
		{"", "", false},
		{"FAILED", "", false},
		{"done", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := entity.ParseStatus(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("ParseStatus(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestStatusCountsAdd(t *testing.T) {
	tests := []struct {
		name   string
		status entity.Status
		n      int
		want   entity.StatusCounts
	}{
		{"pending", entity.StatusPending, 2, entity.StatusCounts{Pending: 2}},
		{"succeeded", entity.StatusSucceeded, 3, entity.StatusCounts{Succeeded: 3}},
		{"failed", entity.StatusFailed, 1, entity.StatusCounts{Failed: 1}},
		{"unknown is ignored", entity.Status("weird"), 5, entity.StatusCounts{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (entity.StatusCounts{}).Add(tt.status, tt.n); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStatusCountsAddAccumulates(t *testing.T) {
	got := entity.StatusCounts{}.Add(entity.StatusPending, 1).Add(entity.StatusPending, 2)
	if got.Pending != 3 {
		t.Fatalf("Pending = %d, want 3", got.Pending)
	}
}

func TestIDValidators(t *testing.T) {
	const body = "01JC3ZM2H7Y9D4E6F8G0J1K2M3" // 26 Crockford characters
	tests := []struct {
		name string
		id   string
		dlv  bool
		ep   bool
	}{
		{"delivery", "dlv_" + body, true, false},
		{"endpoint", "ep_" + body, false, true},
		{"wrong prefix", "msg_" + body, false, false},
		{"25 chars", "dlv_" + body[:25], false, false},
		{"27 chars", "dlv_" + body + "0", false, false},
		{"lowercase", "dlv_" + "01jc3zm2h7y9d4e6f8g0j1k2m3", false, false},
		{"letter I", "dlv_" + body[:25] + "I", false, false},
		{"letter L", "dlv_" + body[:25] + "L", false, false},
		{"letter O", "dlv_" + body[:25] + "O", false, false},
		{"letter U", "dlv_" + body[:25] + "U", false, false},
		{"empty", "", false, false},
		{"trailing newline", "dlv_" + body + "\n", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entity.IsDeliveryID(tt.id); got != tt.dlv {
				t.Errorf("IsDeliveryID = %v, want %v", got, tt.dlv)
			}
			if got := entity.IsEndpointID(tt.id); got != tt.ep {
				t.Errorf("IsEndpointID = %v, want %v", got, tt.ep)
			}
		})
	}
}
