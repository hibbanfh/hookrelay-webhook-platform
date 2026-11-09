package auth_test

import (
	"context"
	"testing"

	"hookrelay/internal/platform/auth"
)

func TestProjectID(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		wantID string
		wantOK bool
	}{
		{"unauthenticated", context.Background(), "", false},
		{"empty id is not authenticated", auth.WithProjectID(context.Background(), ""), "", false},
		{"authenticated", auth.WithProjectID(context.Background(), "proj_1"), "proj_1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := auth.ProjectID(tt.ctx)
			if id != tt.wantID || ok != tt.wantOK {
				t.Fatalf("got (%q,%v), want (%q,%v)", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}
