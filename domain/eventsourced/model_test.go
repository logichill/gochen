package eventsourced_test

import (
	es "gochen/domain/eventsourced"
	"gochen/errors"
	"testing"
)

func TestModelRequiresIsolationAndProtectsInitial(t *testing.T) {
	if err := (es.Model[int, int]{Initial: func() int { return 0 }, Reduce: func(s, e int) (int, error) { return s + e, nil }}).Validate(); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("missing clone: %v", err)
	}
	initial := map[string]int{"balance": 10}
	model := es.Model[map[string]int, int]{Initial: func() map[string]int { return map[string]int{} }, Clone: func(s map[string]int) map[string]int {
		out := map[string]int{}
		for k, v := range s {
			out[k] = v
		}
		return out
	}, Reduce: func(s map[string]int, e int) (map[string]int, error) {
		s["balance"] += e
		if e < 0 {
			return nil, errors.NewCode(errors.Validation, "bad event")
		}
		return s, nil
	}}
	if _, err := model.Replay(initial, []int{1, -2}); err == nil {
		t.Fatal("expected reducer failure")
	}
	if initial["balance"] != 10 {
		t.Fatal("failure changed initial state")
	}
	result, err := model.Replay(initial, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	result["balance"] = 100
	if initial["balance"] != 10 {
		t.Fatal("successful replay shares mutable state")
	}
}
