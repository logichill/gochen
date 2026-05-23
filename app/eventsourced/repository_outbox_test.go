package eventsourced

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/outbox"
	"gochen/eventing/store"
)

// 测试用领域事件
type valueSetOutboxEvent struct{ V int }

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *valueSetOutboxEvent) EventType() string { return "ValueSet" }

// 测试用聚合
type outboxAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

// newOutboxAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*outboxAggregate）
func newOutboxAggregate(id int64) *outboxAggregate {
	a := &outboxAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "OutboxAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyValueSetOutboxEvent 应用 valueSetOutboxEvent。
func (a *outboxAggregate) ApplyValueSetOutboxEvent(evt *valueSetOutboxEvent) {
	a.Value = evt.V
}

// mockOutboxRepo 是最小可用的 IOutboxRepository 测试桩。
type mockOutboxRepo struct {
	savedAggregateID int64
	savedEvents      []eventing.Event[int64]
	calls            int
	saveErr          error
}

// SaveWithEvents ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregateID：对象/实体标识
// - events：事件列表（待追加/发布）（类型：[]eventing.Event[int64]）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockOutboxRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	m.calls++
	m.savedAggregateID = aggregateID
	m.savedEvents = append([]eventing.Event[int64](nil), events...)
	return m.saveErr
}

// ClaimPendingEntries 从存储中查询实体。
//
// 说明：
// - 以下接口方法为占位实现，测试不涉及。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：outbox.OutboxEntry[int64]）
// - err：错误信息（nil 表示成功）
func (m *mockOutboxRepo) ClaimPendingEntries(ctx context.Context, limit int) ([]outbox.OutboxEntry[int64], error) {
	return nil, nil
}

// MarkAsPublished ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockOutboxRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

// MarkAsFailed ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - entryID：对象/实体标识
// - errorMsg：错误信息（类型：string）
// - nextRetryAt：参数值（具体语义见函数上下文）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockOutboxRepo) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	return nil
}

func (m *mockOutboxRepo) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

// DeletePublished 删除对象并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - olderThan：阈值（用于过滤更早的数据）（类型：time.Time）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockOutboxRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

// TestOutboxAwareRepository_Save_UsesOutboxAndMarksCommitted 验证 OutboxAwareRepository Save UsesOutboxAndMarksCommitted。
func TestOutboxAwareRepository_Save_UsesOutboxAndMarksCommitted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	baseStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetOutboxEvent{} }))
	mox := &mockOutboxRepo{}

	storeWithOutbox, err := NewDomainEventStore(DomainEventStoreOptions[*outboxAggregate, int64]{
		AggregateType:    "OutboxAggregate",
		EventStore:       baseStore,
		OutboxRepo:       mox,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*outboxAggregate, int64]("OutboxAggregate", &outboxAggregate{}, AdaptAggregateFactory(newOutboxAggregate), storeWithOutbox)
	require.NoError(t, err)

	agg := newOutboxAggregate(1001)
	require.NoError(t, agg.ApplyAndRecord(&valueSetOutboxEvent{V: 42}))

	require.NoError(t, repo.Save(ctx, agg))

	require.Equal(t, 1, mox.calls)
	require.Equal(t, int64(1001), mox.savedAggregateID)
	require.Len(t, mox.savedEvents, 1)
	require.Equal(t, "ValueSet", mox.savedEvents[0].GetType())
	require.Len(t, agg.GetUncommittedEvents(), 0)
}

// TestOutboxAwareRepository_Save_PropagatesOutboxError 验证 OutboxAwareRepository Save PropagatesOutboxError。
func TestOutboxAwareRepository_Save_PropagatesOutboxError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	baseStore := store.NewMemoryEventStore()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetOutboxEvent{} }))
	mox := &mockOutboxRepo{
		saveErr: fmt.Errorf("outbox save failed"),
	}

	storeWithOutbox, err := NewDomainEventStore(DomainEventStoreOptions[*outboxAggregate, int64]{
		AggregateType:    "OutboxAggregate",
		EventStore:       baseStore,
		OutboxRepo:       mox,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)

	repo, err := newTestEventSourcedRepository[*outboxAggregate, int64]("OutboxAggregate", &outboxAggregate{}, AdaptAggregateFactory(newOutboxAggregate), storeWithOutbox)
	require.NoError(t, err)

	agg := newOutboxAggregate(2002)
	require.NoError(t, agg.ApplyAndRecord(&valueSetOutboxEvent{V: 1}))

	err = repo.Save(ctx, agg)
	require.Error(t, err)
	require.Equal(t, 1, mox.calls)
}
