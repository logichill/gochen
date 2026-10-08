package eventstore_test

import (
	"context"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	"gochen/codec/jsoncodec"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/store/snapshot"
	"gochen/eventing/upcast"
)

// benchSetEvent 用于基准测试的领域事件（用于模拟 SQL store 反序列化后的 map payload）。
type benchSetEvent struct{ V int }

// EventType 返回事件类型标识。
func (e *benchSetEvent) EventType() string { return "BenchSet" }

// benchAggregate 用于基准测试的聚合：
// - 实现 SnapshotData/RestoreFromSnapshotData，避免快照 JSON 反序列化覆盖聚合 ID/基类指针。
type benchAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newBenchAggregate id：对象/实体标识。
func newBenchAggregate(id int64) *benchAggregate {
	a := &benchAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "BenchAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyBenchSetEvent 应用 benchSetEvent。
func (a *benchAggregate) ApplyBenchSetEvent(evt *benchSetEvent) {
	a.Value = evt.V
}

// SnapshotData 返回当前值。
func (a *benchAggregate) SnapshotData() any {
	return struct {
		Value int `json:"value"`
	}{
		Value: a.Value,
	}
}

// RestoreFromSnapshotData data：数据（载荷/对象）（类型：any）。
func (a *benchAggregate) RestoreFromSnapshotData(data any) error {
	var state struct {
		Value int `json:"value"`
	}
	if err := jsoncodec.TranscodeInto(data, &state); err != nil {
		return err
	}
	a.Value = state.Value
	return nil
}

// seedBenchEvents 执行对应操作。
func seedBenchEvents(b *testing.B, s store.IEventStore[int64], aggregateID int64, total int) {
	b.Helper()

	storable := make([]eventing.IStorableEvent[int64], 0, total)
	for i := 1; i <= total; i++ {
		payload := map[string]any{"V": i}
		evt := newTestEvent[int64](aggregateID, "BenchAggregate", "BenchSet", uint64(i), payload)
		storable = append(storable, evt)
	}
	if err := s.AppendEvents(context.TODO(), "BenchAggregate", aggregateID, storable, 0); err != nil {
		b.Fatalf("seed events failed: %v", err)
	}
}

// BenchmarkDomainEventStore_RestoreAggregate_NoSnapshot_StreamAggregate 用于评估 esstore.DomainEventStore RestoreAggregate NoSnapshot StreamAggregate 的性能。
func BenchmarkDomainEventStore_RestoreAggregate_NoSnapshot_StreamAggregate(b *testing.B) {
	reg := registry.NewRegistry()
	_ = reg.Register("BenchSet", func() any { return &benchSetEvent{} })
	upgraders := upcast.NewUpgraderRegistry()

	ctx := context.TODO()
	eventStore := store.NewMemoryEventStore[int64]()
	seedBenchEvents(b, eventStore, 1, 10000)

	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:      esstore.PublishModePersistOnly,
		AggregateType:    "BenchAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	if err != nil {
		b.Fatalf("esstore.NewDomainEventStore failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agg := newBenchAggregate(1)
		if _, err := adapter.RestoreAggregate(ctx, agg); err != nil {
			b.Fatalf("RestoreAggregate failed: %v", err)
		}
	}
}

// BenchmarkDomainEventStore_RestoreAggregate_WithSnapshot_StreamAggregate 用于评估 esstore.DomainEventStore RestoreAggregate WithSnapshot StreamAggregate 的性能。
func BenchmarkDomainEventStore_RestoreAggregate_WithSnapshot_StreamAggregate(b *testing.B) {
	reg := registry.NewRegistry()
	_ = reg.Register("BenchSet", func() any { return &benchSetEvent{} })
	upgraders := upcast.NewUpgraderRegistry()

	ctx := context.TODO()
	eventStore := store.NewMemoryEventStore[int64]()
	seedBenchEvents(b, eventStore, 1, 10000)

	// 创建轻量快照：假设已处理 9000 条事件，仅需重放最后 1000 条。
	snapStore := snapshot.NewMemoryStore[int64]()
	snapMgr := snapshot.NewManager[int64](snapStore, snapshot.DefaultConfig())

	aggForSnap := newBenchAggregate(1)
	for i := 1; i <= 9000; i++ {
		_ = aggForSnap.ApplyEvent(&benchSetEvent{V: i})
	}
	if err := snapMgr.CreateSnapshot(ctx, 1, "BenchAggregate", aggForSnap, aggForSnap.GetVersion()); err != nil {
		b.Fatalf("CreateSnapshot failed: %v", err)
	}

	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:      esstore.PublishModePersistOnly,
		AggregateType:    "BenchAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       eventStore,
		SnapshotManager:  snapMgr,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
		Logger:           nil,
	})
	if err != nil {
		b.Fatalf("esstore.NewDomainEventStore failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agg := newBenchAggregate(1)
		if _, err := adapter.RestoreAggregate(ctx, agg); err != nil {
			b.Fatalf("RestoreAggregate failed: %v", err)
		}
	}
}
