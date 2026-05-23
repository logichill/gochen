package querymatch

import (
	"testing"

	"gochen/db/query"
	"gochen/db/query/querybind"
	"gochen/errors"
)

func TestTextDecodeQueryExprs_RejectsUnsupportedOperator(t *testing.T) {
	var matcher Text
	err := matcher.DecodeQueryExprs([]query.QueryExpr{{
		Op: query.FilterOpIn,
		Values: []query.QueryValue{
			{Type: query.FieldTypeString, String: "alice"},
		},
	}})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestPrefixDecodeQueryExprs_RejectsUnsupportedOperator(t *testing.T) {
	var matcher Prefix
	err := matcher.DecodeQueryExprs([]query.QueryExpr{{
		Op: query.FilterOpNotNull,
	}})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestQuerybindWithTextMatcher_RejectsUnsupportedOperator(t *testing.T) {
	type sample struct {
		Name Text `filter:"name"`
	}

	err := querybind.Bind(query.QueryFilters{
		"name": {{
			Op: query.FilterOpIn,
			Values: []query.QueryValue{
				{Type: query.FieldTypeString, String: "alice"},
			},
		}},
	}, &sample{})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}
