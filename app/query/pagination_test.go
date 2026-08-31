package query

import (
	"math"
	"testing"

	"gochen/errors"
)

func TestPaginationOptions_Offset(t *testing.T) {
	tests := []struct {
		name string
		opts *PaginationOptions
		want int
	}{
		{name: "nil", opts: nil, want: 0},
		{name: "first page", opts: &PaginationOptions{Page: 1, Size: 20}, want: 0},
		{name: "later page", opts: &PaginationOptions{Page: 3, Size: 20}, want: 40},
		{name: "invalid size", opts: &PaginationOptions{Page: 3, Size: 0}, want: 0},
		{name: "overflow", opts: &PaginationOptions{Page: math.MaxInt, Size: 1000}, want: math.MaxInt},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.Offset(); got != tc.want {
				t.Fatalf("expected offset %d, got %d", tc.want, got)
			}
		})
	}
}

func TestPaginationOptions_ValidateRejectsOffsetOverflow(t *testing.T) {
	pageSize := 1000
	maxPage := math.MaxInt/pageSize + 2
	options := &PaginationOptions{Page: maxPage, Size: pageSize}
	if err := options.Validate(0); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for overflowing offset, got %v", err)
	}
}

func TestPaginationOptions_ValidateAcceptsLargestRepresentableOffset(t *testing.T) {
	pageSize := 1000
	options := &PaginationOptions{Page: math.MaxInt/pageSize + 1, Size: pageSize}
	if err := options.Validate(0); err != nil {
		t.Fatalf("expected largest representable offset to pass, got %v", err)
	}
	options.Page++
	if err := options.Validate(0); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected next page to be rejected, got %v", err)
	}
}
