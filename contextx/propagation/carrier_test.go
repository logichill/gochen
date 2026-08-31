package propagation

import (
	"sort"
	"testing"
)

func TestMapCarrierReadWriteAndKeys(t *testing.T) {
	carrier := NewMapCarrier()
	carrier.Set("traceparent", "trace")
	carrier.Set("tenant_id", "tenant-1")

	if got, ok := carrier.Get("traceparent"); !ok || got != "trace" {
		t.Fatalf("Get(traceparent) = %q, %v", got, ok)
	}
	keys := carrier.Keys()
	sort.Strings(keys)
	if len(keys) != 2 || keys[0] != "tenant_id" || keys[1] != "traceparent" {
		t.Fatalf("Keys() = %v", keys)
	}
}

func TestMapCarrierMissingKeyAndNilWrite(t *testing.T) {
	var carrier MapCarrier
	if value, ok := carrier.Get("missing"); ok || value != "" {
		t.Fatalf("Get(missing) = %q, %v", value, ok)
	}
	defer func() {
		if recovered := recover(); recovered != "propagation.MapCarrier.Set called on nil map; use NewMapCarrier" {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	carrier.Set("key", "value")
}
