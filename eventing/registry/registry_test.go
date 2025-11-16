package registry

import (
	"testing"
)

type sampleEvent struct {
	Name string `json:"name"`
}

func TestRegistry_RegisterAndDeserialize(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterWithVersion("TestEvent", 2, func() interface{} { return &sampleEvent{} }); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !r.HasEvent("TestEvent") {
		t.Fatalf("expected type registered")
	}
	if got := r.GetSchemaVersion("TestEvent"); got != 2 {
		t.Fatalf("unexpected schema version: %d", got)
	}

	payload := map[string]interface{}{"name": "demo"}
	typed, err := r.DeserializeFromMap("TestEvent", payload)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	ev, ok := typed.(*sampleEvent)
	if !ok {
		t.Fatalf("unexpected instance type %#v", typed)
	}
	if ev.Name != "demo" {
		t.Fatalf("unexpected value %s", ev.Name)
	}

	types := r.GetRegisteredTypes()
	if len(types) != 1 {
		t.Fatalf("unexpected types length %d", len(types))
	}
}
