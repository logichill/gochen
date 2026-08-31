package eventsourced

import (
	"testing"

	"gochen/errors"
)

type unregisteredAggregate struct {
	*EventSourcedAggregate[int64]
}

func (a *unregisteredAggregate) ApplySupportAggregateEvent(*supportAggregateEvent) {}

func TestMetadataRegistryResolveRequiresRegistration(t *testing.T) {
	registry := NewMetadataRegistry()
	_, err := registry.Resolve(&unregisteredAggregate{}, "unregistered_aggregate")
	if err == nil {
		t.Fatal("expected MetadataRegistry.Resolve to fail when metadata is not registered")
	}
	if !errors.Is(err, errors.Internal) {
		t.Fatalf("expected Internal, got %v", err)
	}
}

func TestInitAggregate_AutoEnsuresMetadata(t *testing.T) {
	registry := NewMetadataRegistry()
	agg := &unregisteredAggregate{}
	esa, err := InitAggregate[int64](registry, agg, 1, "unregistered_aggregate_init")
	if err != nil {
		t.Fatalf("InitAggregate failed: %v", err)
	}
	agg.EventSourcedAggregate = esa

	meta, err := registry.Resolve(&unregisteredAggregate{}, "unregistered_aggregate_init")
	if err != nil {
		t.Fatalf("expected InitAggregate to auto-register metadata, got %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata to be available after InitAggregate")
	}
}

func TestMetadataRegistryCachesMetadata(t *testing.T) {
	registry := NewMetadataRegistry()
	meta1, err := registry.Register(&TestAggregate{}, "TestAggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Register failed: %v", err)
	}
	meta2, err := registry.Register(&TestAggregate{}, "TestAggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Register second call failed: %v", err)
	}
	meta3, err := registry.Resolve(&TestAggregate{}, "TestAggregate")
	if err != nil {
		t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
	}
	if meta1 != meta2 || meta2 != meta3 {
		t.Fatal("expected aggregate metadata registration to reuse the same cached metadata")
	}
}

func TestMetadataRegistryZeroValueCanRegister(t *testing.T) {
	registry := &MetadataRegistry{}
	meta, err := registry.Register(&TestAggregate{}, "TestAggregate")
	if err != nil {
		t.Fatalf("zero value MetadataRegistry.Register failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata from zero value registry")
	}
}
