package querybind

import (
	"reflect"
	"testing"
	"time"

	"gochen/db/query"
)

func TestContract_SchemaAndDecodeShareSingleInputStruct(t *testing.T) {
	type input struct {
		PointIDs []int64                `query:"field=point_id,ops=eq|in,nosort,noselect"`
		Time     query.Range[time.Time] `query:"field=time,ops=gte|lte,nosort,noselect"`
	}

	contract, err := NewContract[input](nil)
	if err != nil {
		t.Fatalf("NewContract failed: %v", err)
	}
	if contract.Schema() == nil {
		t.Fatalf("expected contract schema")
	}
	if _, ok := contract.Schema().Field("point_id"); !ok {
		t.Fatalf("expected point_id field in inferred schema")
	}
	if _, ok := contract.Schema().Field("time"); !ok {
		t.Fatalf("expected time field in inferred schema")
	}

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	bound, err := contract.Decode(query.QueryFilters{
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
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(bound.PointIDs) != 2 || bound.PointIDs[0] != 1 || bound.PointIDs[1] != 2 {
		t.Fatalf("unexpected point ids: %+v", bound.PointIDs)
	}
	if bound.Time.Lower == nil || !bound.Time.Lower.Value.Equal(from) {
		t.Fatalf("unexpected lower bound: %+v", bound.Time.Lower)
	}
	if bound.Time.Upper == nil || !bound.Time.Upper.Value.Equal(to) {
		t.Fatalf("unexpected upper bound: %+v", bound.Time.Upper)
	}
}

func TestContract_DecodeReusesFieldNameMapper(t *testing.T) {
	type input struct {
		UserID int64
	}

	contract, err := NewContract[input](&query.SchemaInferOptions{
		FieldNameMapper: func(field reflect.StructField) string {
			if field.Name == "UserID" {
				return "uid"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("NewContract failed: %v", err)
	}
	if _, ok := contract.Schema().Field("uid"); !ok {
		t.Fatalf("expected mapped field uid in inferred schema")
	}

	bound, err := contract.Decode(query.QueryFilters{
		"uid": {{
			Op:    query.FilterOpEq,
			Value: query.IntValue(42),
		}},
	})
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if bound.UserID != 42 {
		t.Fatalf("expected uid to bind into UserID, got %+v", bound)
	}
}
