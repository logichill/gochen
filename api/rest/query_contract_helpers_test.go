package rest

import (
	"testing"

	"gochen/errors"
)

func TestRejectLegacyParams(t *testing.T) {
	err := RejectLegacyParams(map[string][]string{
		"status": {"OPEN"},
		"page":   {"1"},
	}, "status", "alarm_id")
	if err == nil {
		t.Fatalf("expected flat query param rejection")
	}
}

func TestRejectLegacyParams_IgnoresModernDSL(t *testing.T) {
	err := RejectLegacyParams(map[string][]string{
		"filter": {"status:eq:OPEN"},
		"page":   {"1"},
		"size":   {"20"},
	}, "status", "alarm_id")
	if err != nil {
		t.Fatalf("expected modern DSL to pass, got %v", err)
	}
}

func TestRejectLegacyQueryParams_NilContextReturnsInvalidInput(t *testing.T) {
	err := RejectLegacyQueryParams(nil, "status")
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}
