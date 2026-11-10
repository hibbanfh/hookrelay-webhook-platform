package entity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 123456789, time.UTC)

func longURL(total int) string {
	prefix := "https://example.com/"
	return prefix + strings.Repeat("a", total-len(prefix))
}

func TestNewEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		description string
		wantField   string
	}{
		{name: "https url", url: "https://example.com/hook"},
		{name: "http url", url: "http://example.com/hook"},
		{name: "localhost with a port", url: "http://localhost:9000/hook"},
		{name: "description given", url: "https://example.com/hook", description: "Acme prod"},
		{name: "description of 255 characters", url: "https://example.com/hook", description: strings.Repeat("é", 255)},
		{name: "url of 2048 characters", url: longURL(2048)},
		{name: "url of 2049 characters", url: longURL(2049), wantField: "url"},
		{name: "description of 256 characters", url: "https://example.com/hook", description: strings.Repeat("a", 256), wantField: "description"},
		{name: "empty url", url: "", wantField: "url"},
		{name: "relative url", url: "/hook", wantField: "url"},
		{name: "scheme-less host", url: "example.com/hook", wantField: "url"},
		{name: "ftp scheme", url: "ftp://example.com/hook", wantField: "url"},
		{name: "javascript scheme", url: "javascript:alert(1)", wantField: "url"},
		{name: "file scheme", url: "file:///etc/passwd", wantField: "url"},
		{name: "missing host", url: "https:///hook", wantField: "url"},
		{name: "scheme only", url: "https://", wantField: "url"},
		{name: "userinfo", url: "https://user:pass@example.com/hook", wantField: "url"},
		{name: "username only", url: "https://user@example.com/hook", wantField: "url"},
		{name: "space inside", url: "https://example.com/a b", wantField: "url"},
		{name: "control character", url: "https://example.com/a\x01b", wantField: "url"},
		{name: "NUL", url: "https://example.com/a\x00b", wantField: "url"},
		{name: "leading space", url: " https://example.com/hook", wantField: "url"},
		{name: "invalid UTF-8 in url", url: "https://example.com/\xff", wantField: "url"},
		{name: "replacement character left by the JSON decoder in url", url: "https://example.com/\ufffd", wantField: "url"},
		{name: "port zero", url: "https://example.com:0/x", wantField: "url"},
		{name: "port 65536", url: "https://example.com:65536/x", wantField: "url"},
		{name: "port 99999", url: "https://example.com:99999/x", wantField: "url"},
		{name: "empty port", url: "https://example.com:/x", wantField: "url"},
		{name: "empty port on IPv6", url: "https://[::1]:/x", wantField: "url"},
		{name: "port 1", url: "https://example.com:1/x"},
		{name: "port 65535", url: "https://example.com:65535/x"},
		{name: "NUL in description", url: "https://example.com/hook", description: "a\x00b", wantField: "description"},
		{name: "newline in description", url: "https://example.com/hook", description: "a\nb", wantField: "description"},
		{name: "invalid UTF-8 in description", url: "https://example.com/hook", description: "a\xffb", wantField: "description"},
		{name: "replacement character from the JSON decoder", url: "https://example.com/hook", description: "a�b", wantField: "description"},
		{name: "bidi override in description", url: "https://example.com/hook", description: "a\u202eb", wantField: "description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.NewEndpoint(entity.EndpointSpec{ID: "ep_1", ProjectID: "proj_1", URL: tt.url, Description: tt.description}, now)

			assertValidationField(t, err, tt.wantField)
		})
	}
}

// assertValidationField expects no error when field is "", otherwise a ValidationError on field.
func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *domainerrors.ValidationError
	gotField := ""
	if errors.As(err, &invalid) {
		gotField = invalid.Field
	}
	isUnexpected := err != nil && gotField == ""
	if isUnexpected || gotField != field || (field != "" && !errors.Is(err, domainerrors.ErrInvalid)) {
		t.Fatalf("got %v, want a validation error on %q", err, field)
	}
}

func TestNewEndpointSetsDefaults(t *testing.T) {
	e, err := entity.NewEndpoint(entity.EndpointSpec{ID: "ep_1", ProjectID: "proj_1", URL: "https://example.com/hook", Description: "Acme"}, now)

	want := now.UTC().Truncate(time.Microsecond)
	if err != nil || !e.Enabled || !e.CreatedAt.Equal(want) || !e.UpdatedAt.Equal(want) || e.ProjectID != "proj_1" {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func str(s string) *string { return &s }
func flag(b bool) *bool    { return &b }

func TestIsEndpointID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3", true},
		{"ep_01JC3ZM2H7Y9D4E6F8G0J1K2M", false},
		{"ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3X", false},
		{"ep_01JC3ZM2H7Y9D4E6F8G0J1K2MI", false},
		{"ep_01jc3zm2h7y9d4e6f8g0j1k2m3", false},
		{"proj_01JC3ZM2H7Y9D4E6F8G0J1K2M3", false},
		{"ep_\x0001JC3ZM2H7Y9D4E6F8G0J1K2M3", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := entity.IsEndpointID(tt.id); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func existingEndpoint(t *testing.T) entity.Endpoint {
	t.Helper()
	e, err := entity.NewEndpoint(entity.EndpointSpec{ID: "ep_1", ProjectID: "proj_1", URL: "https://example.com/hook", Description: "old"}, now)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestApplyPatchAppliesOnlyThePresentFields(t *testing.T) {
	later := now.Truncate(time.Microsecond).Add(time.Hour)
	tests := []struct {
		name     string
		patch    entity.Patch
		wantURL  string
		wantDesc string
		wantOn   bool
	}{
		{name: "url only", patch: entity.Patch{URL: str("https://other.example/x")}, wantURL: "https://other.example/x", wantDesc: "old", wantOn: true},
		{name: "description only", patch: entity.Patch{Description: str("new")}, wantURL: "https://example.com/hook", wantDesc: "new", wantOn: true},
		{name: "clear description with empty string", patch: entity.Patch{Description: str("")}, wantURL: "https://example.com/hook", wantDesc: "", wantOn: true},
		{name: "disable", patch: entity.Patch{Enabled: flag(false)}, wantURL: "https://example.com/hook", wantDesc: "old", wantOn: false},
		{name: "same values still succeed", patch: entity.Patch{URL: str("https://example.com/hook")}, wantURL: "https://example.com/hook", wantDesc: "old", wantOn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := existingEndpoint(t)

			got, err := current.ApplyPatch(tt.patch, later)

			want := entity.Endpoint{ID: "ep_1", ProjectID: "proj_1", URL: tt.wantURL, Description: tt.wantDesc, Enabled: tt.wantOn, CreatedAt: current.CreatedAt, UpdatedAt: later}
			if err != nil || got != want {
				t.Fatalf("got %+v (%v), want %+v", got, err, want)
			}
		})
	}
}

func TestApplyPatchRejectsInvalidPatches(t *testing.T) {
	tests := []struct {
		name      string
		patch     entity.Patch
		wantField string
	}{
		{name: "empty patch", patch: entity.Patch{}, wantField: "body"},
		{name: "invalid url", patch: entity.Patch{URL: str("ftp://x")}, wantField: "url"},
		{name: "url too long", patch: entity.Patch{URL: str(longURL(2049))}, wantField: "url"},
		{name: "description too long", patch: entity.Patch{Description: str(strings.Repeat("a", 256))}, wantField: "description"},
		{name: "one invalid field fails the whole patch", patch: entity.Patch{Enabled: flag(false), URL: str("nope")}, wantField: "url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := existingEndpoint(t).ApplyPatch(tt.patch, now)

			assertValidationField(t, err, tt.wantField)
		})
	}
}
