package entity_test

import (
	"testing"

	"hookrelay/internal/message/domain/entity"
)

func TestIsMessageID(t *testing.T) {
	const body = "01JC3ZM2H7Y9D4E6F8G0J1K2M3"
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid", "msg_" + body, true},
		{"wrong prefix", "dlv_" + body, false},
		{"25 chars", "msg_" + body[:25], false},
		{"27 chars", "msg_" + body + "0", false},
		{"lowercase", "msg_01jc3zm2h7y9d4e6f8g0j1k2m3", false},
		{"letter I", "msg_" + body[:25] + "I", false},
		{"letter L", "msg_" + body[:25] + "L", false},
		{"letter O", "msg_" + body[:25] + "O", false},
		{"letter U", "msg_" + body[:25] + "U", false},
		{"empty", "", false},
		{"trailing newline", "msg_" + body + "\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entity.IsMessageID(tt.id); got != tt.want {
				t.Fatalf("IsMessageID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
