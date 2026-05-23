package query

import (
	"testing"
	"time"

	"gochen/errors"
)

func TestParseRange_TimeBounds(t *testing.T) {
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)

	rng, ok, err := ParseRange[time.Time]([]QueryExpr{
		{Op: FilterOpGte, Value: QueryValue{Type: FieldTypeTime, Time: from}},
		{Op: FilterOpLt, Value: QueryValue{Type: FieldTypeTime, Time: to}},
	})
	if err != nil {
		t.Fatalf("ParseRange returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ParseRange to find a range")
	}
	if rng.Lower == nil || !rng.Lower.Inclusive || !rng.Lower.Value.Equal(from) {
		t.Fatalf("unexpected lower bound: %+v", rng.Lower)
	}
	if rng.Upper == nil || rng.Upper.Inclusive || !rng.Upper.Value.Equal(to) {
		t.Fatalf("unexpected upper bound: %+v", rng.Upper)
	}
}

func TestParseRange_EqBecomesExactRange(t *testing.T) {
	rng, ok, err := ParseRange[int64]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeInt, Int: 42},
	}})
	if err != nil {
		t.Fatalf("ParseRange returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected exact range to be detected")
	}
	if rng.Lower == nil || rng.Upper == nil || !rng.Lower.Inclusive || !rng.Upper.Inclusive {
		t.Fatalf("expected exact inclusive range, got %+v", rng)
	}
	if rng.Lower.Value != 42 || rng.Upper.Value != 42 {
		t.Fatalf("expected exact value 42, got %+v", rng)
	}
}

func TestParseRange_RejectsConflictingLowerBounds(t *testing.T) {
	_, _, err := ParseRange[int64]([]QueryExpr{
		{Op: FilterOpGt, Value: QueryValue{Type: FieldTypeInt, Int: 10}},
		{Op: FilterOpGte, Value: QueryValue{Type: FieldTypeInt, Int: 11}},
	})
	if err == nil {
		t.Fatalf("expected ParseRange to reject conflicting lower bounds")
	}
}

func TestRange_BoundAccessors(t *testing.T) {
	from := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 7, 8, 30, 0, 0, time.UTC)
	rng := Range[time.Time]{
		Lower: &Bound[time.Time]{Value: from, Inclusive: true},
		Upper: &Bound[time.Time]{Value: to, Inclusive: false},
	}

	lower := rng.LowerPtr()
	upper := rng.UpperPtr()
	if lower == nil || !lower.Equal(from) {
		t.Fatalf("expected LowerPtr to return %v, got %v", from, lower)
	}
	if upper == nil || !upper.Equal(to) {
		t.Fatalf("expected UpperPtr to return %v, got %v", to, upper)
	}
	if !rng.LowerValue().Equal(from) {
		t.Fatalf("expected LowerValue to return %v, got %v", from, rng.LowerValue())
	}
	if !rng.UpperValue().Equal(to) {
		t.Fatalf("expected UpperValue to return %v, got %v", to, rng.UpperValue())
	}

	empty := Range[time.Time]{}
	if empty.LowerPtr() != nil || empty.UpperPtr() != nil {
		t.Fatalf("expected empty range ptr accessors to return nil")
	}
	if !empty.LowerValue().IsZero() || !empty.UpperValue().IsZero() {
		t.Fatalf("expected empty range value accessors to return zero values")
	}
}

func TestQuerySchemaDecodeFilters_RejectsInvalidRangeCombination(t *testing.T) {
	schema := NewQuerySchema(TimeField("created_at"))
	_, err := schema.DecodeFilters([]Filter{
		{Field: "created_at", Op: FilterOpGt, Value: "2026-04-08T00:00:00Z"},
		{Field: "created_at", Op: FilterOpLte, Value: "2026-04-01T00:00:00Z"},
	})
	if err == nil {
		t.Fatalf("expected DecodeFilters to reject an inverted range")
	}
}

func TestParseRange_RejectsSignedIntegerOverflow(t *testing.T) {
	_, _, err := ParseRange[int8]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeInt, Int: 1000},
	}})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestParseRange_RejectsUnsignedIntegerOverflow(t *testing.T) {
	_, _, err := ParseRange[uint8]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeInt, Int: 300},
	}})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestParseRange_RejectsIncompatibleScalarValueTypes(t *testing.T) {
	tests := []struct {
		name  string
		parse func() error
	}{
		{
			name: "string to int",
			parse: func() error {
				_, _, err := ParseRange[int64]([]QueryExpr{{
					Op:    FilterOpEq,
					Value: QueryValue{Type: FieldTypeString, String: "42"},
				}})
				return err
			},
		},
		{
			name: "string to time",
			parse: func() error {
				_, _, err := ParseRange[time.Time]([]QueryExpr{{
					Op:    FilterOpEq,
					Value: QueryValue{Type: FieldTypeString, String: "2026-04-06T00:00:00Z"},
				}})
				return err
			},
		},
		{
			name: "bool to string",
			parse: func() error {
				_, _, err := ParseRange[string]([]QueryExpr{{
					Op:    FilterOpEq,
					Value: QueryValue{Type: FieldTypeBool, Bool: true},
				}})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.parse()
			if !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}

func TestParseRange_RejectsMissingQueryValueType(t *testing.T) {
	_, _, err := ParseRange[int64]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Int: 42},
	}})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
	if got := err.Error(); got != "[INVALID_INPUT] query range compare failed: [INVALID_INPUT] query range: query value type is required" {
		t.Fatalf("expected missing type error, got %v", err)
	}
}

func TestParseRange_AllowsIntegerValueForFloatTarget(t *testing.T) {
	rng, ok, err := ParseRange[float64]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeInt, Int: 42},
	}})
	if err != nil {
		t.Fatalf("ParseRange returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ParseRange to find a range")
	}
	if rng.Lower == nil || rng.Upper == nil || rng.Lower.Value != 42 || rng.Upper.Value != 42 {
		t.Fatalf("expected exact float range 42, got %+v", rng)
	}
}
