package query

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

type InferEmbeddedFields struct {
	TenantID int64 `query:"ops=eq,nosort"`
}

type inferTestEntity struct {
	InferEmbeddedFields

	ID        int64  `json:"id" query:"nofilter,select"`
	Name      string `json:"display_name"`
	Active    bool
	Age       int       `query:"ops=eq|gte"`
	Status    string    `query:"type=enum,field=status_code,ops=eq"`
	CreatedAt time.Time `query:"layout=2006-01-02,sort"`
	Hidden    string    `json:"-"`
	Secret    string    `query:"-"`
	Version   uint64    `query:"nofilter,nosort,noselect"`
	private   string
}

func TestInferQuerySchema_DefaultsFromStruct(t *testing.T) {
	schema, err := InferQuerySchema[*inferTestEntity](nil)
	if err != nil {
		t.Fatalf("InferQuerySchema returned error: %v", err)
	}
	if schema == nil {
		t.Fatalf("expected inferred schema")
	}

	checkField(t, schema, "id", FieldTypeInt, nil, true, true, "")
	checkField(t, schema, "name", FieldTypeString, []FilterOp{FilterOpEq, FilterOpLike}, true, true, "")
	checkField(t, schema, "active", FieldTypeBool, []FilterOp{FilterOpEq}, true, true, "")
	checkField(t, schema, "age", FieldTypeInt, []FilterOp{FilterOpEq, FilterOpGte}, true, true, "")
	checkField(t, schema, "status_code", FieldTypeEnum, []FilterOp{FilterOpEq}, true, true, "")
	checkField(t, schema, "created_at", FieldTypeTime, []FilterOp{FilterOpEq, FilterOpGt, FilterOpGte, FilterOpLt, FilterOpLte}, true, true, "2006-01-02")
	checkField(t, schema, "tenant_id", FieldTypeInt, []FilterOp{FilterOpEq}, false, true, "")

	if _, ok := schema.Field("hidden"); ok {
		t.Fatalf("expected json:\"-\" field to be skipped")
	}
	if _, ok := schema.Field("secret"); ok {
		t.Fatalf("expected query:\"-\" field to be skipped")
	}
	if _, ok := schema.Field("version"); ok {
		t.Fatalf("expected fully disabled field to be skipped")
	}

	if got := schema.SelectableFieldNames(); !slices.Equal(got, []string{"active", "age", "created_at", "id", "name", "status_code", "tenant_id"}) {
		t.Fatalf("unexpected selectable fields: %v", got)
	}
}

func TestInferQuerySchema_FieldNameMapper(t *testing.T) {
	schema, err := InferQuerySchema[*inferTestEntity](&SchemaInferOptions{
		FieldNameMapper: func(field reflect.StructField) string {
			return "x_" + field.Name
		},
	})
	if err != nil {
		t.Fatalf("InferQuerySchema returned error: %v", err)
	}

	if _, ok := schema.Field("x_Name"); !ok {
		t.Fatalf("expected mapped field name x_Name")
	}
	if _, ok := schema.Field("x_TenantID"); !ok {
		t.Fatalf("expected embedded field to also use mapper")
	}
	if _, ok := schema.Field("status_code"); !ok {
		t.Fatalf("expected explicit field override to win over mapper")
	}
	if _, ok := schema.Field("x_Status"); ok {
		t.Fatalf("expected explicit field override to suppress mapper result")
	}
}

func TestInferQuerySchema_InvalidTagRejected(t *testing.T) {
	type badEntity struct {
		Name string `query:"ops=boom"`
	}

	_, err := InferQuerySchema[*badEntity](nil)
	if err == nil {
		t.Fatalf("expected invalid tag error")
	}
}

func TestInferQuerySchema_DuplicateFieldRejected(t *testing.T) {
	type duplicateEntity struct {
		Foo string `query:"field=value"`
		Bar string `query:"field=value"`
	}

	_, err := InferQuerySchema[*duplicateEntity](nil)
	if err == nil {
		t.Fatalf("expected duplicate field error")
	}
}

func TestInferQuerySchema_SnakeCaseHandlesInitialisms(t *testing.T) {
	type acronymEntity struct {
		HTTPStatus int
		UserID     int64
		OAuth2ID   string
	}

	schema, err := InferQuerySchema[*acronymEntity](nil)
	if err != nil {
		t.Fatalf("InferQuerySchema returned error: %v", err)
	}

	if _, ok := schema.Field("http_status"); !ok {
		t.Fatalf("expected http_status field")
	}
	if _, ok := schema.Field("user_id"); !ok {
		t.Fatalf("expected user_id field")
	}
	if _, ok := schema.Field("oauth2_id"); !ok {
		t.Fatalf("expected oauth2_id field")
	}
}

func TestInferQuerySchema_SupportsSliceAndRangeInputFields(t *testing.T) {
	type queryInput struct {
		PointIDs []int64          `query:"field=point_id,ops=eq|in,nosort,noselect"`
		Time     Range[time.Time] `query:"field=time,ops=gte|lte,nosort,noselect"`
		Keyword  string           `query:"field=keyword,ops=eq|like,nosort,noselect"`
	}

	schema, err := InferQuerySchema[*queryInput](nil)
	if err != nil {
		t.Fatalf("InferQuerySchema returned error: %v", err)
	}

	checkField(t, schema, "point_id", FieldTypeInt, []FilterOp{FilterOpEq, FilterOpIn}, false, false, "")
	checkField(t, schema, "time", FieldTypeTime, []FilterOp{FilterOpGte, FilterOpLte}, false, false, "")
	checkField(t, schema, "keyword", FieldTypeString, []FilterOp{FilterOpEq, FilterOpLike}, false, false, "")
}

func TestInferQuerySchema_DefaultSliceOpsStayWithinBindableOperators(t *testing.T) {
	type queryInput struct {
		IDs   []int64
		Times []time.Time
	}

	schema, err := InferQuerySchema[*queryInput](nil)
	if err != nil {
		t.Fatalf("InferQuerySchema returned error: %v", err)
	}

	checkField(t, schema, "ids", FieldTypeInt, []FilterOp{FilterOpEq, FilterOpIn}, true, true, "")
	checkField(t, schema, "times", FieldTypeTime, []FilterOp{FilterOpEq, FilterOpIn}, true, true, "")
}

func TestInferQuerySchema_ReturnsErrorOnInvalidTag(t *testing.T) {
	type badEntity struct {
		Name string `query:"ops=boom"`
	}

	_, err := InferQuerySchema[*badEntity](nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func checkField(t *testing.T, schema *QuerySchema, name string, typ FieldType, ops []FilterOp, sortable, selectable bool, layout string) {
	t.Helper()

	field, ok := schema.Field(name)
	if !ok {
		t.Fatalf("expected field %q to exist", name)
	}
	if field.Type != typ {
		t.Fatalf("expected field %q type %q, got %q", name, typ, field.Type)
	}
	if !slices.Equal(field.FilterOps, ops) {
		t.Fatalf("expected field %q ops %v, got %v", name, ops, field.FilterOps)
	}
	if field.Sortable != sortable {
		t.Fatalf("expected field %q sortable=%v, got %v", name, sortable, field.Sortable)
	}
	if field.Selectable != selectable {
		t.Fatalf("expected field %q selectable=%v, got %v", name, selectable, field.Selectable)
	}
	if field.TimeLayout != layout {
		t.Fatalf("expected field %q layout %q, got %q", name, layout, field.TimeLayout)
	}
}
