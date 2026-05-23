package query

import "testing"

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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.Offset(); got != tc.want {
				t.Fatalf("expected offset %d, got %d", tc.want, got)
			}
		})
	}
}
