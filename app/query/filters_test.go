package query

import (
	"testing"

	"gochen/errors"
)

// TestFilterBuilder_BuildAndApply 验证 FilterBuilder BuildAndApply。
func TestFilterBuilder_BuildAndApply(t *testing.T) {
	b := NewFilterBuilder().
		Eq("status", "active").
		Like("name", "alice").
		In("role", "admin", "user")

	out := b.Build()
	if len(out) != 3 {
		t.Fatalf("expected 3 filters, got %d", len(out))
	}
	if out[0].Field != "status" || out[0].Op != FilterOpEq || out[0].Value != "active" {
		t.Fatalf("unexpected filter[0]: %+v", out[0])
	}
	if out[1].Field != "name" || out[1].Op != FilterOpLike || out[1].Value != "alice" {
		t.Fatalf("unexpected filter[1]: %+v", out[1])
	}
	if out[2].Field != "role" || out[2].Op != FilterOpIn {
		t.Fatalf("unexpected filter[2]: %+v", out[2])
	}
	if len(out[2].Values) != 2 || out[2].Values[0] != "admin" || out[2].Values[1] != "user" {
		t.Fatalf("unexpected in values: %#v", out[2].Values)
	}

	// Build 返回副本
	out[0].Value = "mutated"
	out2 := b.Build()
	if out2[0].Value != "active" {
		t.Fatalf("expected Build() to return a copy")
	}

	params := &QueryRequest{
		Filters: QueryFilters{
			"existing": {{
				Op:    FilterOpEq,
				Value: QueryValue{Type: FieldTypeString, Normalized: "1", String: "1"},
			}},
		},
	}
	if err := b.Apply(params); err != nil {
		t.Fatalf("unexpected Apply error: %v", err)
	}
	if len(params.Filters) != 4 {
		t.Fatalf("unexpected Apply result: %+v", params.Filters)
	}
	existing := params.Filters.Get("existing")
	if len(existing) != 1 || existing[0].Value.Normalized != "1" {
		t.Fatalf("unexpected existing filter: %+v", existing)
	}
	status := params.Filters.Get("status")
	if len(status) != 1 || status[0].Value.Normalized != "active" {
		t.Fatalf("unexpected appended filter: %+v", status)
	}
}

// TestFilterBuilder_EmptyFieldNoop 验证 FilterBuilder EmptyFieldNoop。
func TestFilterBuilder_EmptyFieldNoop(t *testing.T) {
	b := NewFilterBuilder().
		Eq("", "1").
		Like("", "x").
		Gt("", "1").
		In("", "a")
	if b.Build() != nil {
		t.Fatalf("expected nil filters")
	}
}

// TestDecodeAdapterFilters_RejectsRangeOperators 验证 DecodeAdapterFilters 拒绝范围操作符。
func TestDecodeAdapterFilters_RejectsRangeOperators(t *testing.T) {
	for _, op := range []FilterOp{FilterOpGt, FilterOpGte, FilterOpLt, FilterOpLte} {
		filters := []Filter{{Field: "age", Op: op, Value: "18"}}
		_, err := DecodeAdapterFilters(filters)
		if err == nil {
			t.Errorf("expected error for op %s, got nil", op)
		}
		if !errors.Is(err, errors.InvalidInput) {
			t.Errorf("expected InvalidInput for op %s, got %v", op, err)
		}
	}

	// 允许的操作符仍然正常
	allowed := []struct {
		filter Filter
	}{
		{Filter{Field: "name", Op: FilterOpEq, Value: "alice"}},
		{Filter{Field: "name", Op: FilterOpNe, Value: "bob"}},
		{Filter{Field: "name", Op: FilterOpLike, Value: "ali%"}},
		{Filter{Field: "role", Op: FilterOpIn, Values: []string{"admin", "user"}}},
		{Filter{Field: "role", Op: FilterOpNotIn, Values: []string{"guest"}}},
		{Filter{Field: "name", Op: FilterOpIsNull}},
		{Filter{Field: "name", Op: FilterOpNotNull}},
	}
	for _, tc := range allowed {
		result, err := DecodeAdapterFilters([]Filter{tc.filter})
		if err != nil {
			t.Errorf("unexpected error for op %s: %v", tc.filter.Op, err)
		}
		if result == nil {
			t.Errorf("expected non-nil result for op %s", tc.filter.Op)
		}
	}
}

// TestDecodeAdapterFilters_ApplyPropagatesRangeError 验证 Apply 传播范围操作符错误。
func TestDecodeAdapterFilters_ApplyPropagatesRangeError(t *testing.T) {
	b := NewFilterBuilder().Gt("age", "18")
	err := b.Apply(&QueryRequest{})
	if err == nil {
		t.Fatalf("expected error from Apply with range operator")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}
