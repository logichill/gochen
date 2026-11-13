package registry

import (
	"testing"

	"gochen/errors"
)

type distinctSampleEvent sampleEvent

func TestRegistryValidateEventRequiresRegisteredGoType(t *testing.T) {
	r := NewRegistry()
	factoryCalls := 0
	if err := r.Register("Sample", func() any {
		factoryCalls++
		return &sampleEvent{}
	}); err != nil {
		t.Fatal(err)
	}
	value := sampleEvent{Name: "value"}
	pointer := &value
	var typedNil *sampleEvent
	for _, tc := range []struct {
		name  string
		event any
		valid bool
	}{
		{name: "value", event: value, valid: true},
		{name: "pointer", event: pointer, valid: true},
		{name: "different value", event: distinctSampleEvent(value)},
		{name: "different pointer", event: &distinctSampleEvent{}},
		{name: "same JSON shape", event: struct {
			Name string `json:"name"`
		}{Name: "value"}},
		{name: "extra pointer", event: &pointer},
		{name: "map", event: map[string]any{"name": "value"}},
		{name: "nil"},
		{name: "typed nil", event: typedNil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := r.ValidateEvent("Sample", tc.event)
			if tc.valid && err != nil {
				t.Fatalf("valid registered type rejected: %v", err)
			}
			if !tc.valid && !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("want InvalidInput, got %v", err)
			}
		})
	}
	if value.Name != "value" || factoryCalls != 1 {
		t.Fatalf("validation mutated event or invoked factory: value=%+v calls=%d", value, factoryCalls)
	}
	if err := r.ValidateEvent("Unknown", pointer); !errors.Is(err, errors.NotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
	r.Unregister("Sample")
	if err := r.ValidateEvent("Sample", pointer); !errors.Is(err, errors.NotFound) {
		t.Fatalf("unregistered type accepted: %v", err)
	}
	var nilRegistry *Registry
	if err := nilRegistry.ValidateEvent("Sample", pointer); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("nil registry: %v", err)
	}
}
