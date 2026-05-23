package query

import "testing"

func TestQueryFilters_DeepCopyNestedValues(t *testing.T) {
	originalExpr := QueryExpr{
		Op: FilterOpIn,
		Values: []QueryValue{
			{Type: FieldTypeString, Normalized: "admin", String: "admin"},
			{Type: FieldTypeString, Normalized: "user", String: "user"},
		},
	}
	filters := QueryFilters(nil).Append("role", originalExpr)

	originalExpr.Values[0].String = "mutated-source"
	got := filters.Get("role")
	if len(got) != 1 || len(got[0].Values) != 2 {
		t.Fatalf("unexpected Get result: %+v", got)
	}
	if got[0].Values[0].String != "admin" {
		t.Fatalf("expected Append to isolate source expr values, got %+v", got[0].Values)
	}

	got[0].Values[0].String = "mutated-get"
	again := filters.Get("role")
	if again[0].Values[0].String != "admin" {
		t.Fatalf("expected Get to deep copy nested values, got %+v", again[0].Values)
	}

	cloned := filters.Clone()
	cloned["role"][0].Values[0].String = "mutated-clone"
	if filters["role"][0].Values[0].String != "admin" {
		t.Fatalf("expected Clone to deep copy nested values, got %+v", filters["role"][0].Values)
	}

	merged := QueryFilters(nil).Merge(filters)
	merged["role"][0].Values[0].String = "mutated-merge"
	if filters["role"][0].Values[0].String != "admin" {
		t.Fatalf("expected Merge to deep copy nested values, got %+v", filters["role"][0].Values)
	}
}
