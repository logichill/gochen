package eventsourced

import "testing"

func newTestMetadataRegistry(tb testing.TB) *MetadataRegistry {
	tb.Helper()
	registry := NewMetadataRegistry()
	mustRegister := func(sample any, aggregateType string) {
		tb.Helper()
		if _, err := registry.Register(sample, aggregateType); err != nil {
			tb.Fatalf("register metadata %s failed: %v", aggregateType, err)
		}
	}
	mustRegister(&TestAggregate{}, "TestAggregate")
	mustRegister(&autoApplyAggregate{}, "AutoApply")
	mustRegister(&supportAggregate{}, "support_aggregate")
	return registry
}
