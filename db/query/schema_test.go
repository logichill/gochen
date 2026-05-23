package query

import (
	"testing"
	"time"

	"gochen/errors"
)

// TestQuerySchema_NormalizeFilters 验证 QuerySchema NormalizeFilters。
func TestQuerySchema_NormalizeFilters(t *testing.T) {
	schema := NewQuerySchema(
		BoolField("active"),
		TimeField("created_at"),
	)

	filters, err := schema.NormalizeFilters([]Filter{
		{Field: "active", Op: FilterOpEq, Value: "TRUE"},
		{Field: "created_at", Op: FilterOpGte, Value: "2026-03-27T10:00:00Z"},
	})
	if err != nil {
		t.Fatalf("NormalizeFilters returned error: %v", err)
	}
	if got := filters[0].Value; got != "true" {
		t.Fatalf("expected bool value to normalize to true, got %q", got)
	}
	if got := filters[1].Value; got != "2026-03-27T10:00:00Z" {
		t.Fatalf("expected time value to stay RFC3339, got %q", got)
	}
}

// TestQuerySchema_InvalidOperatorRejected 验证 QuerySchema InvalidOperatorRejected。
func TestQuerySchema_InvalidOperatorRejected(t *testing.T) {
	schema := NewQuerySchema(BoolField("active"))

	_, err := schema.NormalizeFilters([]Filter{
		{Field: "active", Op: FilterOpLike, Value: "true"},
	})
	if err == nil {
		t.Fatalf("expected invalid operator error")
	}
}

// TestQuerySchema_DecodeFilters 验证 QuerySchema DecodeFilters。
func TestQuerySchema_DecodeFilters(t *testing.T) {
	schema := NewQuerySchema(
		BoolField("active"),
		IntField("age"),
		TimeField("created_at"),
	)

	decoded, err := schema.DecodeFilters([]Filter{
		{Field: "active", Op: FilterOpEq, Value: "true"},
		{Field: "age", Op: FilterOpGte, Value: "18"},
		{Field: "created_at", Op: FilterOpLte, Value: "2026-03-27T10:00:00Z"},
	})
	if err != nil {
		t.Fatalf("DecodeFilters returned error: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 decoded filter fields, got %d", len(decoded))
	}
	active := decoded.Get("active")
	if len(active) != 1 || !active[0].Value.Bool {
		t.Fatalf("expected bool decoded value to be true")
	}
	if active[0].Value.Normalized != "true" {
		t.Fatalf("expected bool normalized value true, got %q", active[0].Value.Normalized)
	}
	age := decoded.Get("age")
	if len(age) != 1 || age[0].Value.Int != 18 {
		t.Fatalf("expected int decoded value 18, got %+v", age)
	}
	if got := age[0].Value.Any(); got != int64(18) {
		t.Fatalf("expected Any() to expose int64(18), got %#v", got)
	}
	created := decoded.Get("created_at")
	if len(created) != 1 || created[0].Value.Time.IsZero() {
		t.Fatalf("expected time decoded value to be populated")
	}
}

func TestQuerySchema_DecodeFiltersRejectsDuplicateNonRangeExpressions(t *testing.T) {
	schema, err := InferQuerySchema[struct {
		Username string `query:"ops=eq|like"`
	}](nil)
	if err != nil {
		t.Fatalf("InferQuerySchema failed: %v", err)
	}

	_, err = schema.DecodeFilters([]Filter{
		{Field: "username", Op: FilterOpEq, Value: "alice"},
		{Field: "username", Op: FilterOpLike, Value: "ali"},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestQueryValueHelpers(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		value      QueryValue
		wantType   FieldType
		wantAny    any
		normalized string
	}{
		{name: "string", value: StringValue("openai"), wantType: FieldTypeString, wantAny: "openai", normalized: "openai"},
		{name: "enum", value: EnumValue("ACTIVE"), wantType: FieldTypeEnum, wantAny: "ACTIVE", normalized: "ACTIVE"},
		{name: "int", value: IntValue(7), wantType: FieldTypeInt, wantAny: int64(7), normalized: "7"},
		{name: "float", value: FloatValue(3.5), wantType: FieldTypeFloat, wantAny: 3.5, normalized: "3.5"},
		{name: "bool", value: BoolValue(true), wantType: FieldTypeBool, wantAny: true, normalized: "true"},
		{name: "time", value: TimeValue(now), wantType: FieldTypeTime, wantAny: now, normalized: "2026-04-06T12:30:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value.Type != tt.wantType {
				t.Fatalf("expected type %q, got %q", tt.wantType, tt.value.Type)
			}
			if got := tt.value.Any(); got != tt.wantAny {
				t.Fatalf("expected Any()=%#v, got %#v", tt.wantAny, got)
			}
			if tt.value.Normalized != tt.normalized {
				t.Fatalf("expected normalized %q, got %q", tt.normalized, tt.value.Normalized)
			}
		})
	}
}

// TestQuerySchema_ValidateSortsAndFields 验证 QuerySchema ValidateSortsAndFields。
func TestQuerySchema_ValidateSortsAndFields(t *testing.T) {
	schema := NewQuerySchema(
		StringField("name"),
		TimeField("created_at", AllowSort()),
		StringField("id", AllowSelect()),
	)

	if err := schema.ValidateSorts([]Sort{{Field: "created_at", Direction: DESC}}); err != nil {
		t.Fatalf("ValidateSorts returned error: %v", err)
	}
	if err := schema.ValidateFields([]string{"id"}); err != nil {
		t.Fatalf("ValidateFields returned error: %v", err)
	}

	if err := schema.ValidateSorts([]Sort{{Field: "name", Direction: ASC}}); err == nil {
		t.Fatalf("expected unsortable field to be rejected")
	}
	if err := schema.ValidateFields([]string{"name"}); err == nil {
		t.Fatalf("expected unselectable field to be rejected")
	}
}
