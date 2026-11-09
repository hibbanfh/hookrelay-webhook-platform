package auth_test

import (
	"context"
	"testing"

	"hookrelay/internal/platform/auth"
)

func TestUserID(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		wantID string
		wantOK bool
	}{
		{"unauthenticated", context.Background(), "", false},
		{"empty id is not authenticated", auth.WithUserID(context.Background(), ""), "", false},
		{"authenticated", auth.WithUserID(context.Background(), "user-1"), "user-1", true},
		{"a project ID is not a user ID", auth.WithProjectID(context.Background(), "proj_1"), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := auth.UserID(tt.ctx)
			if id != tt.wantID || ok != tt.wantOK {
				t.Fatalf("got (%q,%v), want (%q,%v)", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}
