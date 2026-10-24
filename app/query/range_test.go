package query

import (
	"math"
	"strings"
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

func TestParseRange_RejectsNaNAndInf(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)

	// Single bound with NaN
	_, _, err := ParseRange[float64]([]QueryExpr{{
		Op:    FilterOpGt,
		Value: QueryValue{Type: FieldTypeFloat, Float: nan},
	}})
	if err == nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for NaN, got %v", err)
	}

	// Single bound with Inf
	_, _, err = ParseRange[float64]([]QueryExpr{{
		Op:    FilterOpLt,
		Value: QueryValue{Type: FieldTypeFloat, Float: inf},
	}})
	if err == nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for Inf, got %v", err)
	}

	// Two bounds with NaN
	_, _, err = ParseRange[float64]([]QueryExpr{
		{Op: FilterOpGte, Value: QueryValue{Type: FieldTypeFloat, Float: 0}},
		{Op: FilterOpLte, Value: QueryValue{Type: FieldTypeFloat, Float: nan}},
	})
	if err == nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for range with NaN, got %v", err)
	}
}

func TestParseRange_Float32Overflow(t *testing.T) {
	overflowVal := float64(math.MaxFloat32) * 2

	_, _, err := ParseRange[float32]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeFloat, Float: overflowVal},
	}})
	if err == nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for float32 overflow, got %v", err)
	}

	// Valid float32 should succeed
	rng, ok, err := ParseRange[float32]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeFloat, Float: 3.14},
	}})
	if err != nil || !ok {
		t.Fatalf("expected valid float32 to succeed, got rng=%+v ok=%v err=%v", rng, ok, err)
	}
}

// 范围字段上的非范围操作符不得被静默忽略（strict 校验）。
func TestQuerySchemaDecodeFilters_RejectsNonRangeOperatorOnRangeField(t *testing.T) {
	for _, field := range []QueryField{
		IntField("age"),
		FloatField("score"),
		TimeField("created_at"),
		StringField("username", WithFilterOps(FilterOpGt, FilterOpIsNull)),
		EnumField("status", WithFilterOps(FilterOpGt, FilterOpIsNull)),
	} {
		schema := NewQuerySchema(field)
		_, err := schema.DecodeFilters([]Filter{
			{Field: field.Name, Op: FilterOpGt, Value: "1"},
			{Field: field.Name, Op: FilterOpIsNull},
		})
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("field %s: expected invalid input for mixed non-range operator, got %v", field.Name, err)
		}
	}
}

// 字符串/枚举字段在显式授予范围操作符后支持自动范围合并（字典序比较）。
func TestQuerySchemaDecodeFilters_StringAndEnumAutoRange(t *testing.T) {
	rangeOps := []FieldOption{WithFilterOps(
		FilterOpEq, FilterOpGte, FilterOpLt,
	)}
	schema := NewQuerySchema(StringField("username", rangeOps...))
	decoded, err := schema.DecodeFilters([]Filter{
		{Field: "username", Op: FilterOpGte, Value: "alice"},
		{Field: "username", Op: FilterOpLt, Value: "bob"},
	})
	if err != nil {
		t.Fatalf("expected string auto-range to be accepted, got %v", err)
	}
	if len(decoded.Get("username")) != 2 {
		t.Fatalf("expected both expressions decoded, got %d", len(decoded.Get("username")))
	}

	schema = NewQuerySchema(EnumField("status", rangeOps...))
	_, err = schema.DecodeFilters([]Filter{
		{Field: "status", Op: FilterOpGte, Value: "active"},
		{Field: "status", Op: FilterOpLt, Value: "disabled"},
	})
	if err != nil {
		t.Fatalf("expected enum auto-range to be accepted, got %v", err)
	}
}

// 布尔字段不支持区间合并，多个表达式必须拒绝。
func TestQuerySchemaDecodeFilters_BoolDoesNotSupportAutoRange(t *testing.T) {
	schema := NewQuerySchema(BoolField("enabled"))
	_, err := schema.DecodeFilters([]Filter{
		{Field: "enabled", Op: FilterOpEq, Value: "true"},
		{Field: "enabled", Op: FilterOpEq, Value: "false"},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input for multiple bool expressions, got %v", err)
	}
}

// 验证 IntField / StringField / EnumField 在 DecodeFilters 下执行单条 LIKE、IN、IS NULL、NE 时正常通过，不被误拒。
func TestQuerySchemaDecodeFilters_AllowsSingleNonRangeOperator(t *testing.T) {
	schema := NewQuerySchema(
		IntField("age"),
		StringField("username"),
		EnumField("status"),
	)

	// IntField: single NE, IN, IS NULL
	for _, f := range []Filter{
		{Field: "age", Op: FilterOpNe, Value: "10"},
		{Field: "age", Op: FilterOpIn, Values: []string{"10", "20"}},
		{Field: "age", Op: FilterOpIsNull},
	} {
		decoded, err := schema.DecodeFilters([]Filter{f})
		if err != nil {
			t.Fatalf("expected filter %+v to pass, got: %v", f, err)
		}
		if len(decoded.Get(f.Field)) != 1 {
			t.Fatalf("expected 1 decoded expression for %+v, got %d", f, len(decoded.Get(f.Field)))
		}
	}

	// StringField: single LIKE, IN, IS NULL, NE
	for _, f := range []Filter{
		{Field: "username", Op: FilterOpLike, Value: "%alice%"},
		{Field: "username", Op: FilterOpIn, Values: []string{"alice", "bob"}},
		{Field: "username", Op: FilterOpIsNull},
		{Field: "username", Op: FilterOpNe, Value: "charlie"},
	} {
		decoded, err := schema.DecodeFilters([]Filter{f})
		if err != nil {
			t.Fatalf("expected filter %+v to pass, got: %v", f, err)
		}
		if len(decoded.Get(f.Field)) != 1 {
			t.Fatalf("expected 1 decoded expression for %+v, got %d", f, len(decoded.Get(f.Field)))
		}
	}

	// EnumField: single LIKE, IN, IS NULL, NE
	for _, f := range []Filter{
		{Field: "status", Op: FilterOpLike, Value: "act%"},
		{Field: "status", Op: FilterOpIn, Values: []string{"active", "pending"}},
		{Field: "status", Op: FilterOpIsNull},
		{Field: "status", Op: FilterOpNe, Value: "disabled"},
	} {
		decoded, err := schema.DecodeFilters([]Filter{f})
		if err != nil {
			t.Fatalf("expected filter %+v to pass, got: %v", f, err)
		}
		if len(decoded.Get(f.Field)) != 1 {
			t.Fatalf("expected 1 decoded expression for %+v, got %d", f, len(decoded.Get(f.Field)))
		}
	}
}

func TestParseRange_RejectsInterfaceTargetType(t *testing.T) {
	_, _, err := ParseRange[any]([]QueryExpr{{
		Op:    FilterOpEq,
		Value: QueryValue{Type: FieldTypeInt, Int: 42},
	}})
	if err == nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for interface target type, got %v", err)
	}
	if !strings.Contains(err.Error(), "query range does not support uninstantiated interface target type") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
