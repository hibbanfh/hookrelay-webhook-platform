package entity_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
)

func TestParseSignatureSet(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    entity.SignatureSet
		wantErr error
	}{
		{name: "one entry", header: "v1,abc", want: entity.SignatureSet{"v1,abc"}},
		{name: "two entries", header: "v1,abc v1,def", want: entity.SignatureSet{"v1,abc", "v1,def"}},
		{name: "extra white space", header: "  v1,abc \t v1,def ", want: entity.SignatureSet{"v1,abc", "v1,def"}},
		{name: "other versions are ignored", header: "v1a,xyz v1,abc", want: entity.SignatureSet{"v1,abc"}},
		{name: "empty", header: "", wantErr: domainerrors.ErrInvalidSignature},
		{name: "only blanks", header: "   ", wantErr: domainerrors.ErrInvalidSignature},
		{name: "only another version", header: "v1a,xyz", wantErr: domainerrors.ErrInvalidSignature},
		{name: "version without a value", header: "v1,", wantErr: domainerrors.ErrInvalidSignature},
		{name: "no comma", header: "v1abc", wantErr: domainerrors.ErrInvalidSignature},
		{name: "ten entries", header: strings.TrimSpace(strings.Repeat("v1,a ", entity.MaxSignatures)), want: slices.Repeat(entity.SignatureSet{"v1,a"}, entity.MaxSignatures)},
		{name: "eleven entries", header: strings.TrimSpace(strings.Repeat("v1,a ", entity.MaxSignatures+1)), wantErr: domainerrors.ErrInvalidSignature},
		{name: "eleven entries counted before filtering", header: strings.Repeat("v2,a ", entity.MaxSignatures) + "v1,a", wantErr: domainerrors.ErrInvalidSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := entity.ParseSignatureSet(tt.header)
			if !errors.Is(err, tt.wantErr) || !slices.Equal(got, tt.want) {
				t.Fatalf("ParseSignatureSet(%q) = %v, %v; want %v, %v", tt.header, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSignatureSetContains(t *testing.T) {
	set := entity.SignatureSet{"v1,first", "v1,middle", "v1,last"}
	tests := []struct {
		name     string
		expected string
		want     bool
	}{
		{name: "match first", expected: "v1,first", want: true},
		{name: "match middle", expected: "v1,middle", want: true},
		{name: "match last", expected: "v1,last", want: true},
		{name: "absent", expected: "v1,other"},
		{name: "different length", expected: "v1,firstt"},
		{name: "prefix of an entry", expected: "v1,fir"},
		{name: "empty", expected: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := set.Contains(tt.expected); got != tt.want {
				t.Fatalf("Contains(%q) = %v, want %v", tt.expected, got, tt.want)
			}
		})
	}
}

func TestEmptySignatureSetContainsNothing(t *testing.T) {
	if (entity.SignatureSet{}).Contains("v1,a") {
		t.Fatal("an empty set must not contain anything")
	}
}
