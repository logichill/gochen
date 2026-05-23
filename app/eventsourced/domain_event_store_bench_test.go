package eventsourced

import (
	"context"
	"testing"

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
//
// 返回：
// - result：文本结果
func (e *benchSetEvent) EventType() string { return "BenchSet" }

// benchAggregate 用于基准测试的聚合：
// - 实现 SnapshotData/RestoreFromSnapshotData，避免快照 JSON 反序列化覆盖聚合 ID/基类指针。
type benchAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newBenchAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*benchAggregate）
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
//
// 返回：
// - result：底层对象（类型：any）
func (a *benchAggregate) SnapshotData() any {
	return struct {
		Value int `json:"value"`
	}{
		Value: a.Value,
	}
}

// RestoreFromSnapshotData data：数据（载荷/对象）（类型：any）。
//
// 参数：
//
// 返回：
// - err：错误信息（nil 表示成功）
func (a *benchAggregate) RestoreFromSnapshotData(data any) error {
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	if v, ok := m["value"]; ok {
		switch typed := v.(type) {
		case float64:
			a.Value = int(typed)
		case int:
			a.Value = typed
		}
	}
	return nil
}

// seedBenchEvents 执行对应操作。
//
// 参数：
// - s：参数值
// - aggregateID：对象/实体标识
// - total：参数值（具体语义见函数上下文）（类型：int）
func seedBenchEvents(b *testing.B, s store.IEventStore[int64], aggregateID int64, total int) {
	b.Helper()

	storable := make([]eventing.IStorableEvent[int64], 0, total)
	for i := 1; i <= total; i++ {
		payload := map[string]any{"V": i}
		evt := eventing.NewEvent[int64](aggregateID, "BenchAggregate", "BenchSet", uint64(i), payload)
		storable = append(storable, evt)
	}
	if err := s.AppendEvents(context.TODO(), aggregateID, storable, 0); err != nil {
		b.Fatalf("seed events failed: %v", err)
	}
}

// BenchmarkDomainEventStore_RestoreAggregate_NoSnapshot_StreamAggregate 用于评估 DomainEventStore RestoreAggregate NoSnapshot StreamAggregate 的性能。
func BenchmarkDomainEventStore_RestoreAggregate_NoSnapshot_StreamAggregate(b *testing.B) {
	reg := registry.NewRegistry()
	_ = reg.Register("BenchSet", func() any { return &benchSetEvent{} })
	upgraders := upcast.NewUpgraderRegistry()

	ctx := context.TODO()
	eventStore := store.NewMemoryEventStore()
	seedBenchEvents(b, eventStore, 1, 10000)

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*benchAggregate, int64]{
		AggregateType:    "BenchAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	if err != nil {
		b.Fatalf("NewDomainEventStore failed: %v", err)
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

// BenchmarkDomainEventStore_RestoreAggregate_WithSnapshot_StreamAggregate 用于评估 DomainEventStore RestoreAggregate WithSnapshot StreamAggregate 的性能。
func BenchmarkDomainEventStore_RestoreAggregate_WithSnapshot_StreamAggregate(b *testing.B) {
	reg := registry.NewRegistry()
	_ = reg.Register("BenchSet", func() any { return &benchSetEvent{} })
	upgraders := upcast.NewUpgraderRegistry()

	ctx := context.TODO()
	eventStore := store.NewMemoryEventStore()
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

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*benchAggregate, int64]{
		AggregateType:    "BenchAggregate",
		EventStore:       eventStore,
		SnapshotManager:  snapMgr,
		PublishEvents:    false,
		EventBus:         nil,
		OutboxRepo:       nil,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
		Logger:           nil,
	})
	if err != nil {
		b.Fatalf("NewDomainEventStore failed: %v", err)
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
