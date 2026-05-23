package eventsourced

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/store"
)

// 测试用领域事件
type valueSetEvent struct{ V int }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *valueSetEvent) EventType() string { return "ValueSet" }

// 测试用聚合
type testAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newTestAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*testAggregate）
func newTestAggregate(id int64) *testAggregate {
	a := &testAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "TestAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyValueSetEvent 应用 valueSetEvent。
func (a *testAggregate) ApplyValueSetEvent(evt *valueSetEvent) {
	a.Value = evt.V
}

// TestEventSourcedRepository_SaveAndLoad 验证 EventSourcedRepository SaveAndLoad。
func TestEventSourcedRepository_SaveAndLoad(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*testAggregate, int64]{
		AggregateType:    "TestAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*testAggregate]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), adapter)
	require.NoError(t, err)
	require.NotNil(t, repo)

	agg := newTestAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&valueSetEvent{V: 42}))

	require.NoError(t, repo.Save(ctx, agg))

	loaded, err := repo.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 42, loaded.Value)
	require.Equal(t, uint64(1), loaded.GetVersion())

	exists, err := repo.Exists(ctx, 1)
	require.NoError(t, err)
	require.True(t, exists)

	version, err := repo.GetAggregateVersion(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
}
