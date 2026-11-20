package entity_test

import (
	"strings"
	"testing"

	"hookrelay/internal/demoreceiver/domain/entity"
)

func TestIsProjectID(t *testing.T) {
	const body = "01JC3ZK6Q8V2W7T4N5R9XBYH0M"
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{name: "valid", id: "proj_" + body, want: true},
		{name: "all digits", id: "proj_" + strings.Repeat("0", 26), want: true},
		{name: "lowercase", id: "proj_" + strings.ToLower(body)},
		{name: "25 characters", id: "proj_" + body[:25]},
		{name: "27 characters", id: "proj_" + body + "0"},
		{name: "letter I", id: "proj_" + "I" + body[1:]},
		{name: "letter L", id: "proj_" + "L" + body[1:]},
		{name: "letter O", id: "proj_" + "O" + body[1:]},
		{name: "letter U", id: "proj_" + "U" + body[1:]},
		{name: "endpoint prefix", id: "ep_" + body},
		{name: "no prefix", id: body},
		{name: "empty", id: ""},
		{name: "prefix only", id: "proj_"},
		{name: "trailing newline", id: "proj_" + body + "\n"},
		{name: "non ASCII of the right byte length", id: "proj_" + body[:22] + "éé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entity.IsProjectID(tt.id); got != tt.want {
				t.Fatalf("IsProjectID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
