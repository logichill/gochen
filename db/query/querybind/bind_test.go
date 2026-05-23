package querybind

import (
	"testing"
	"time"

	"gochen/db/query"
	"gochen/errors"
)

type decodeText struct {
	Value string
	Op    query.FilterOp
}

func (d *decodeText) DecodeQueryExprs(exprs []query.QueryExpr) error {
	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "decodeText expects exactly one expression")
	}
	d.Value = exprs[0].Value.String
	d.Op = exprs[0].Op
	return nil
}

func TestBind_DefaultRulesAndCustomDecoder(t *testing.T) {
	now := time.Now().UTC().Round(0)
	type sample struct {
		ID        int64      `filter:"id"`
		Status    string     `filter:"status"`
		CreatedAt time.Time  `filter:"created_at"`
		Tags      []string   `filter:"tags"`
		Name      decodeText `filter:"name"`
	}

	var dst sample
	err := Bind(query.QueryFilters{
		"id": {{
			Op:    query.FilterOpEq,
			Value: query.IntValue(7),
		}},
		"status": {{
			Op:    query.FilterOpEq,
			Value: query.StringValue("active"),
		}},
		"created_at": {{
			Op:    query.FilterOpEq,
			Value: query.TimeValue(now),
		}},
		"tags": {{
			Op: query.FilterOpIn,
			Values: []query.QueryValue{
				query.StringValue("a"),
				query.StringValue("b"),
			},
		}},
		"name": {{
			Op:    query.FilterOpLike,
			Value: query.StringValue("alice"),
		}},
	}, &dst)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}

	if dst.ID != 7 || dst.Status != "active" {
		t.Fatalf("unexpected scalar bind result: %+v", dst)
	}
	if !dst.CreatedAt.Equal(now) {
		t.Fatalf("expected created_at %v, got %v", now, dst.CreatedAt)
	}
	if len(dst.Tags) != 2 || dst.Tags[0] != "a" || dst.Tags[1] != "b" {
		t.Fatalf("unexpected slice bind result: %+v", dst.Tags)
	}
	if dst.Name.Value != "alice" || dst.Name.Op != query.FilterOpLike {
		t.Fatalf("unexpected custom decoder result: %+v", dst.Name)
	}
}

func TestBind_DefaultRuleRejectsUnsupportedOperator(t *testing.T) {
	type sample struct {
		Name string `filter:"name"`
	}

	err := Bind(query.QueryFilters{
		"name": {{
			Op:    query.FilterOpLike,
			Value: query.QueryValue{Type: query.FieldTypeString, String: "alice"},
		}},
	}, &sample{})
	if err == nil {
		t.Fatalf("expected Bind to reject unsupported default operator")
	}
}

func TestBind_DefaultSnakeCaseHandlesInitialism(t *testing.T) {
	type sample struct {
		UserID int64
	}

	var dst sample
	err := Bind(query.QueryFilters{
		"user_id": {{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeInt, Int: 42},
		}},
	}, &dst)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if dst.UserID != 42 {
		t.Fatalf("expected user_id to bind, got %+v", dst)
	}
}

func TestBind_UsesQueryTagFieldNameWhenFilterTagIsAbsent(t *testing.T) {
	type sample struct {
		PointIDs []int64                `query:"field=point_id,ops=eq|in,nosort,noselect"`
		Time     query.Range[time.Time] `query:"field=time,ops=gte|lte,nosort,noselect"`
	}

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	var dst sample
	err := Bind(query.QueryFilters{
		"point_id": {{
			Op: query.FilterOpIn,
			Values: []query.QueryValue{
				query.IntValue(1),
				query.IntValue(2),
			},
		}},
		"time": {
			{Op: query.FilterOpGte, Value: query.TimeValue(from)},
			{Op: query.FilterOpLte, Value: query.TimeValue(to)},
		},
	}, &dst)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if len(dst.PointIDs) != 2 || dst.PointIDs[0] != 1 || dst.PointIDs[1] != 2 {
		t.Fatalf("unexpected point ids: %+v", dst.PointIDs)
	}
	if dst.Time.Lower == nil || !dst.Time.Lower.Value.Equal(from) {
		t.Fatalf("unexpected lower bound: %+v", dst.Time.Lower)
	}
	if dst.Time.Upper == nil || !dst.Time.Upper.Value.Equal(to) {
		t.Fatalf("unexpected upper bound: %+v", dst.Time.Upper)
	}
}

func TestBind_CustomPointerDecoder(t *testing.T) {
	type sample struct {
		Name *decodeText `filter:"name"`
	}

	var dst sample
	err := Bind(query.QueryFilters{
		"name": {{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeString, String: "alice"},
		}},
	}, &dst)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if dst.Name == nil || dst.Name.Value != "alice" || dst.Name.Op != query.FilterOpEq {
		t.Fatalf("expected pointer decoder to be allocated and filled, got %+v", dst.Name)
	}
}

func TestBind_CustomDecoderRejectsMultipleExpressions(t *testing.T) {
	type sample struct {
		Name decodeText `filter:"name"`
	}

	err := Bind(query.QueryFilters{
		"name": {
			{Op: query.FilterOpEq, Value: query.QueryValue{Type: query.FieldTypeString, String: "alice"}},
			{Op: query.FilterOpLike, Value: query.QueryValue{Type: query.FieldTypeString, String: "ali"}},
		},
	}, &sample{})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestBind_RangeField(t *testing.T) {
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)

	type sample struct {
		CreatedAt query.Range[time.Time] `filter:"created_at"`
		Score     query.Range[int64]     `filter:"score"`
	}

	var dst sample
	err := Bind(query.QueryFilters{
		"created_at": {
			{Op: query.FilterOpGte, Value: query.QueryValue{Type: query.FieldTypeTime, Time: from}},
			{Op: query.FilterOpLt, Value: query.QueryValue{Type: query.FieldTypeTime, Time: to}},
		},
		"score": {
			{Op: query.FilterOpEq, Value: query.QueryValue{Type: query.FieldTypeInt, Int: 90}},
		},
	}, &dst)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}

	if dst.CreatedAt.Lower == nil || !dst.CreatedAt.Lower.Value.Equal(from) || !dst.CreatedAt.Lower.Inclusive {
		t.Fatalf("unexpected created_at lower bound: %+v", dst.CreatedAt.Lower)
	}
	if dst.CreatedAt.Upper == nil || !dst.CreatedAt.Upper.Value.Equal(to) || dst.CreatedAt.Upper.Inclusive {
		t.Fatalf("unexpected created_at upper bound: %+v", dst.CreatedAt.Upper)
	}
	if dst.Score.Lower == nil || dst.Score.Upper == nil || dst.Score.Lower.Value != 90 || dst.Score.Upper.Value != 90 {
		t.Fatalf("expected exact score range, got %+v", dst.Score)
	}
}

func TestBind_UnsignedIntegerRejectsNegativeValue(t *testing.T) {
	type sample struct {
		ID uint64 `filter:"id"`
	}

	err := Bind(query.QueryFilters{
		"id": {{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeInt, Int: -1},
		}},
	}, &sample{})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestBind_RejectsIncompatibleScalarValueTypes(t *testing.T) {
	type sample struct {
		ID        int64     `filter:"id"`
		Active    bool      `filter:"active"`
		CreatedAt time.Time `filter:"created_at"`
	}

	tests := []struct {
		name    string
		filters query.QueryFilters
	}{
		{
			name: "string to int",
			filters: query.QueryFilters{
				"id": {{
					Op:    query.FilterOpEq,
					Value: query.QueryValue{Type: query.FieldTypeString, String: "42"},
				}},
			},
		},
		{
			name: "string to bool",
			filters: query.QueryFilters{
				"active": {{
					Op:    query.FilterOpEq,
					Value: query.QueryValue{Type: query.FieldTypeString, String: "true"},
				}},
			},
		},
		{
			name: "string to time",
			filters: query.QueryFilters{
				"created_at": {{
					Op:    query.FilterOpEq,
					Value: query.QueryValue{Type: query.FieldTypeString, String: "2026-04-06T00:00:00Z"},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Bind(tt.filters, &sample{})
			if !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
}

func TestBind_RejectsMissingQueryValueType(t *testing.T) {
	type sample struct {
		ID int64 `filter:"id"`
	}

	err := Bind(query.QueryFilters{
		"id": {{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Int: 42},
		}},
	}, &sample{})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
	if got := err.Error(); got != "[INVALID_INPUT] querybind decode failed: [INVALID_INPUT] querybind: query value type is required" {
		t.Fatalf("expected missing type error, got %v", err)
	}
}

func TestBind_AllowsIntegerValueForFloatTarget(t *testing.T) {
	type sample struct {
		Score float64 `filter:"score"`
	}

	var dst sample
	err := Bind(query.QueryFilters{
		"score": {{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeInt, Int: 42},
		}},
	}, &dst)
	if err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if dst.Score != 42 {
		t.Fatalf("expected decoded float score 42, got %+v", dst)
	}
}

func TestDecode_GenericHelper(t *testing.T) {
	type sample struct {
		ID int64 `filter:"id"`
	}

	dst, err := Decode[sample](query.QueryFilters{
		"id": {{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeInt, Int: 42},
		}},
	})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if dst.ID != 42 {
		t.Fatalf("expected decoded id 42, got %+v", dst)
	}
}
