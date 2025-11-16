package eventsourced

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/outbox"
)

// mockOutboxRepo 是最小可用的 IOutboxRepository 测试桩
type mockOutboxRepo struct {
	savedAggregateID int64
	savedEvents      []eventing.Event
	calls            int
	saveErr          error
}

func (m *mockOutboxRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event) error {
	m.calls++
	m.savedAggregateID = aggregateID
	// 复制一份，避免外部修改
	m.savedEvents = append([]eventing.Event(nil), events...)
	return m.saveErr
}

// 以下接口方法为占位实现，测试不涉及
func (m *mockOutboxRepo) GetPendingEntries(ctx context.Context, limit int) ([]outbox.OutboxEntry, error) {
	return nil, nil
}
func (m *mockOutboxRepo) MarkAsPublished(ctx context.Context, entryID int64) error { return nil }
func (m *mockOutboxRepo) MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error {
	return nil
}
func (m *mockOutboxRepo) DeletePublished(ctx context.Context, olderThan time.Time) error { return nil }

func TestOutboxAwareRepository_Save_UsesOutboxAndMarksCommitted(t *testing.T) {
	ctx := context.Background()
	// 基础仓储（用于读取与快照复用；此处不走基础 Save）
	store := NewMockEventStore()
	base, err := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       func(id int64) *TestAggregate { return NewTestAggregate(id) },
		EventStore:    store,
	})
	require.NoError(t, err)

	// 装饰器 + mock outbox
	mox := &mockOutboxRepo{}
	repo, err := NewOutboxAwareRepository(base, mox)
	require.NoError(t, err)

	// 构造聚合与未提交事件
	agg := NewTestAggregate(1001)
	evt := eventing.NewDomainEvent(agg.GetID(), "TestAggregate", "ValueSet", uint64(agg.GetVersion()+1), 42)
	require.NoError(t, agg.ApplyAndRecord(evt))

	// 保存（应走 Outbox）
	err = repo.Save(ctx, agg)
	require.NoError(t, err)

	// 断言 Outbox 被调用且携带 1 条事件
	require.Equal(t, 1, mox.calls)
	require.Equal(t, int64(1001), mox.savedAggregateID)
	require.Len(t, mox.savedEvents, 1)
	require.Equal(t, "ValueSet", mox.savedEvents[0].GetType())

	// 断言事件已被标记为已提交
	require.Len(t, agg.GetUncommittedEvents(), 0)
}
