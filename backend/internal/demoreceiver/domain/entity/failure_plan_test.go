package entity_test

import (
	"errors"
	"testing"

	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
)

func TestNewFailurePlan(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		wantErr error
	}{
		{name: "minus one", n: -1, wantErr: domainerrors.ErrInvalid},
		{name: "zero never fails", n: 0},
		{name: "two", n: 2},
		{name: "the maximum", n: entity.MaxFailFirst},
		{name: "one above the maximum", n: entity.MaxFailFirst + 1, wantErr: domainerrors.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.NewFailurePlan(tt.n)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewFailurePlan(%d) error = %v, want %v", tt.n, err, tt.wantErr)
			}
		})
	}
}

func TestNewFailurePlanNamesTheField(t *testing.T) {
	_, err := entity.NewFailurePlan(11)

	var invalid *domainerrors.ValidationError
	if !errors.As(err, &invalid) || invalid.Field != "fail_first" {
		t.Fatalf("got %v, want a ValidationError on fail_first", err)
	}
}

func TestFailurePlanShouldFail(t *testing.T) {
	tests := []struct {
		name      string
		failFirst int
		request   int
		want      bool
	}{
		{name: "first of two", failFirst: 2, request: 1, want: true},
		{name: "second of two", failFirst: 2, request: 2, want: true},
		{name: "third of two", failFirst: 2, request: 3},
		{name: "zero budget", failFirst: 0, request: 1},
		{name: "last of ten", failFirst: 10, request: 10, want: true},
		{name: "eleventh of ten", failFirst: 10, request: 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := entity.NewFailurePlan(tt.failFirst)
			if err != nil {
				t.Fatal(err)
			}
			if got := plan.ShouldFail(tt.request); got != tt.want {
				t.Fatalf("ShouldFail(%d) with budget %d = %v, want %v", tt.request, tt.failFirst, got, tt.want)
			}
		})
	}
}
