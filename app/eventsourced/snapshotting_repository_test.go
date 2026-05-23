package eventsourced

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing/store"
	"gochen/eventing/store/snapshot"
	"gochen/logging"
)

type snapEvent struct{ V int }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *snapEvent) EventType() string { return "SnapSet" }

type snapshotAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newSnapshotAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*snapshotAggregate）
func newSnapshotAggregate(id int64) *snapshotAggregate {
	a := &snapshotAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "SnapAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplySnapEvent 应用 snapEvent。
func (a *snapshotAggregate) ApplySnapEvent(evt *snapEvent) {
	a.Value = evt.V
}

// SnapshotData 返回当前值。
//
// 返回：
// - result：底层对象（类型：any）
func (a *snapshotAggregate) SnapshotData() any {
	return map[string]any{"value": a.Value}
}

// RestoreFromSnapshotData data：数据（载荷/对象）（类型：any）。
//
// 参数：
//
// 返回：
// - err：错误信息（nil 表示成功）
func (a *snapshotAggregate) RestoreFromSnapshotData(data any) error {
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

// TestSnapshottingRepository_Save_CreatesSnapshotWhenPolicyMatches 验证 SnapshottingRepository Save CreatesSnapshotWhenPolicyMatches。
func TestSnapshottingRepository_Save_CreatesSnapshotWhenPolicyMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("SnapSet", func() any { return &snapEvent{} }))
	snapStore := snapshot.NewMemoryStore[int64]()
	snapMgr := snapshot.NewManager[int64](snapStore, &snapshot.Config{Frequency: 2, Enabled: true})

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*snapshotAggregate, int64]{
		AggregateType:    "SnapAggregate",
		EventStore:       eventStore,
		SnapshotManager:  snapMgr,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	baseRepo, err := newTestEventSourcedRepository[*snapshotAggregate, int64]("SnapAggregate", &snapshotAggregate{}, AdaptAggregateFactory(newSnapshotAggregate), adapter)
	require.NoError(t, err)

	repo, err := NewSnapshottingRepository[*snapshotAggregate, int64]("SnapAggregate", baseRepo, SnapshottingRepositoryOptions[int64]{
		SnapshotManager: snapMgr,
		FailOnError:     true,
		Logger:          logging.NewNoopLogger(),
	})
	require.NoError(t, err)

	agg := newSnapshotAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&snapEvent{V: 1}))
	require.NoError(t, agg.ApplyAndRecord(&snapEvent{V: 2}))

	require.NoError(t, repo.Save(ctx, agg))

	snap, err := snapStore.FindSnapshot(ctx, "SnapAggregate", 1)
	require.NoError(t, err)
	require.Equal(t, uint64(2), snap.Version)
}

type failingSnapshotStore struct{}

// SaveSnapshot ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - ss：参数值
//
// 返回：
// - err：错误信息（nil 表示成功）
func (f failingSnapshotStore) SaveSnapshot(ctx context.Context, ss snapshot.Snapshot[int64]) error {
	return fmt.Errorf("save snapshot failed")
}

// FindSnapshot 从存储中查询数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateType：聚合类型（类型：string）
// - aggregateID：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*snapshot.Snapshot[int64]）
// - err：错误信息（nil 表示成功）
func (f failingSnapshotStore) FindSnapshot(ctx context.Context, aggregateType string, aggregateID int64) (*snapshot.Snapshot[int64], error) {
	return nil, fmt.Errorf("not found")
}

// DeleteSnapshot 删除数据并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateType：聚合类型（类型：string）
// - aggregateID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (f failingSnapshotStore) DeleteSnapshot(ctx context.Context, aggregateType string, aggregateID int64) error {
	return nil
}

// ListSnapshots 从存储中查询数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateType：聚合类型（类型：string）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：snapshot.Snapshot[int64]）
// - err：错误信息（nil 表示成功）
func (f failingSnapshotStore) ListSnapshots(ctx context.Context, aggregateType string, limit int) ([]snapshot.Snapshot[int64], error) {
	return nil, nil
}

// CleanupSnapshots ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - retentionPeriod：参数值（具体语义见函数上下文）（类型：time.Duration）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (f failingSnapshotStore) CleanupSnapshots(ctx context.Context, retentionPeriod time.Duration) error {
	return nil
}

// TestSnapshottingRepository_Save_SnapshotFailureDoesNotFailByDefault 验证 SnapshottingRepository Save SnapshotFailureDoesNotFailByDefault。
func TestSnapshottingRepository_Save_SnapshotFailureDoesNotFailByDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("SnapSet", func() any { return &snapEvent{} }))

	snapMgr := snapshot.NewManager[int64](failingSnapshotStore{}, &snapshot.Config{Frequency: 1, Enabled: true})

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*snapshotAggregate, int64]{
		AggregateType:    "SnapAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	baseRepo, err := newTestEventSourcedRepository[*snapshotAggregate, int64]("SnapAggregate", &snapshotAggregate{}, AdaptAggregateFactory(newSnapshotAggregate), adapter)
	require.NoError(t, err)

	repo, err := NewSnapshottingRepository[*snapshotAggregate, int64]("SnapAggregate", baseRepo, SnapshottingRepositoryOptions[int64]{
		SnapshotManager: snapMgr,
		FailOnError:     false,
		Logger:          logging.NewNoopLogger(),
	})
	require.NoError(t, err)

	agg := newSnapshotAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&snapEvent{V: 1}))

	require.NoError(t, repo.Save(ctx, agg))
}

// TestSnapshottingRepository_Save_SnapshotFailureFailsWhenConfigured 验证 SnapshottingRepository Save SnapshotFailureFailsWhenConfigured。
func TestSnapshottingRepository_Save_SnapshotFailureFailsWhenConfigured(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("SnapSet", func() any { return &snapEvent{} }))

	snapMgr := snapshot.NewManager[int64](failingSnapshotStore{}, &snapshot.Config{Frequency: 1, Enabled: true})

	adapter, err := NewDomainEventStore(DomainEventStoreOptions[*snapshotAggregate, int64]{
		AggregateType:    "SnapAggregate",
		EventStore:       eventStore,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	baseRepo, err := newTestEventSourcedRepository[*snapshotAggregate, int64]("SnapAggregate", &snapshotAggregate{}, AdaptAggregateFactory(newSnapshotAggregate), adapter)
	require.NoError(t, err)

	repo, err := NewSnapshottingRepository[*snapshotAggregate, int64]("SnapAggregate", baseRepo, SnapshottingRepositoryOptions[int64]{
		SnapshotManager: snapMgr,
		FailOnError:     true,
		Logger:          logging.NewNoopLogger(),
	})
	require.NoError(t, err)

	agg := newSnapshotAggregate(1)
	require.NoError(t, agg.ApplyAndRecord(&snapEvent{V: 1}))

	err = repo.Save(ctx, agg)
	require.Error(t, err)
}
