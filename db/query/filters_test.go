package query

import "testing"

// TestFilterBuilder_BuildAndApply 验证 FilterBuilder BuildAndApply。
func TestFilterBuilder_BuildAndApply(t *testing.T) {
	b := NewFilterBuilder().
		Eq("status", "active").
		Like("name", "alice").
		Gt("age", "18").
		In("role", "admin", "user")

	out := b.Build()
	if len(out) != 4 {
		t.Fatalf("expected 4 filters, got %d", len(out))
	}
	if out[0].Field != "status" || out[0].Op != FilterOpEq || out[0].Value != "active" {
		t.Fatalf("unexpected filter[0]: %+v", out[0])
	}
	if out[1].Field != "name" || out[1].Op != FilterOpLike || out[1].Value != "alice" {
		t.Fatalf("unexpected filter[1]: %+v", out[1])
	}
	if out[2].Field != "age" || out[2].Op != FilterOpGt || out[2].Value != "18" {
		t.Fatalf("unexpected filter[2]: %+v", out[2])
	}
	if out[3].Field != "role" || out[3].Op != FilterOpIn {
		t.Fatalf("unexpected filter[3]: %+v", out[3])
	}
	if len(out[3].Values) != 2 || out[3].Values[0] != "admin" || out[3].Values[1] != "user" {
		t.Fatalf("unexpected in values: %#v", out[3].Values)
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
	b.Apply(params)
	if len(params.Filters) != 5 {
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
