package entity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

func TestNewProject(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		in       string
		wantName string
		wantErr  bool
	}{
		{name: "valid", in: "Acme", wantName: "Acme"},
		{name: "trimmed", in: "  Acme  ", wantName: "Acme"},
		{name: "exactly 100 chars", in: strings.Repeat("a", 100), wantName: strings.Repeat("a", 100)},
		{name: "100 multibyte chars", in: strings.Repeat("é", 100), wantName: strings.Repeat("é", 100)},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: " \t\n ", wantErr: true},
		{name: "101 chars", in: strings.Repeat("a", 101), wantErr: true},
		{name: "101 chars between spaces", in: " " + strings.Repeat("a", 101) + " ", wantErr: true},
		{name: "NUL byte", in: "a\x00b", wantErr: true},
		{name: "trailing NUL survives trimming", in: "ab\x00", wantErr: true},
		{name: "bell", in: "a\x07b", wantErr: true},
		{name: "embedded newline", in: "a\nb", wantErr: true},
		{name: "embedded tab", in: "a\tb", wantErr: true},
		{name: "DEL", in: "a\x7fb", wantErr: true},
		{name: "C1 control", in: "a\u0085b", wantErr: true},
		{name: "surrounding newlines are trimmed", in: "\nAcme\n", wantName: "Acme"},
		{name: "inner double space kept", in: "Acme  Corp", wantName: "Acme  Corp"},
		{name: "CJK and emoji are not control characters", in: "日本😀", wantName: "日本😀"},
		{name: "inner NBSP accepted", in: "Acme\u00a0Corp", wantName: "Acme\u00a0Corp"},
		{name: "ZWJ family emoji accepted", in: "\U0001F468\u200d\U0001F469\u200d\U0001F467", wantName: "\U0001F468\u200d\U0001F469\u200d\U0001F467"},
		{name: "bidi LRE", in: "a\u202ab", wantErr: true},
		{name: "bidi RLO", in: "a\u202eb", wantErr: true},
		{name: "bidi isolate LRI", in: "a\u2066b", wantErr: true},
		{name: "bidi isolate PDI", in: "a\u2069b", wantErr: true},
		{name: "invalid UTF-8", in: "a\xffb", wantErr: true},
		{name: "truncated UTF-8 sequence", in: "ab\xe6\x97", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := entity.NewProject("proj_1", tt.in, now)
			if tt.wantErr {
				var ve *domainerrors.ValidationError
				if !errors.Is(err, domainerrors.ErrInvalid) || !errors.As(err, &ve) || ve.Field != "name" {
					t.Fatalf("want validation error on name, got %v", err)
				}
				return
			}
			if err != nil || p.Name != tt.wantName || p.ID != "proj_1" || !p.CreatedAt.Equal(now) {
				t.Fatalf("got %+v, %v", p, err)
			}
		})
	}
}

func TestNewOwnedProject(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		owner   string
		pname   string
		wantErr error
	}{
		{name: "valid", owner: "user-1", pname: entity.DefaultProjectName},
		{name: "empty owner", owner: "", pname: "Acme", wantErr: domainerrors.ErrInvalid},
		{name: "blank owner", owner: "  ", pname: "Acme", wantErr: domainerrors.ErrInvalid},
		{name: "invalid name", owner: "user-1", pname: "", wantErr: domainerrors.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := entity.NewOwnedProject("proj_1", tt.owner, tt.pname, now)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (p.OwnerID != tt.owner || p.ID != "proj_1" || p.Name != tt.pname) {
				t.Fatalf("got %+v", p)
			}
		})
	}
}
