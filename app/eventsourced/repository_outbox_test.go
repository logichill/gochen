package eventsourced

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/eventing"
	"gochen/eventing/outbox"
	"gochen/eventing/store"
)

// 测试用领域事件
type valueSetOutboxEvent struct{ V int }

func (e *valueSetOutboxEvent) EventType() string { return "ValueSet" }

// 测试用聚合
type outboxAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Value int
}

func newOutboxAggregate(id int64) *outboxAggregate {
	return &outboxAggregate{
		EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "OutboxAggregate"),
	}
}

func (a *outboxAggregate) ApplyEvent(evt domain.IDomainEvent) error {
	if e, ok := evt.(*valueSetOutboxEvent); ok {
		a.Value = e.V
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

// mockOutboxRepo 是最小可用的 IOutboxRepository 测试桩。
type mockOutboxRepo struct {
	savedAggregateID int64
	savedEvents      []eventing.Event
	calls            int
	saveErr          error
}

func (m *mockOutboxRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event) error {
	m.calls++
	m.savedAggregateID = aggregateID
	m.savedEvents = append([]eventing.Event(nil), events...)
	return m.saveErr
}

// 以下接口方法为占位实现，测试不涉及。
func (m *mockOutboxRepo) GetPendingEntries(ctx context.Context, limit int) ([]outbox.OutboxEntry, error) {
	return nil, nil
}

func (m *mockOutboxRepo) MarkAsPublished(ctx context.Context, entryID int64) error {
	return nil
}

func (m *mockOutboxRepo) MarkAsFailed(ctx context.Context, entryID int64, errorMsg string, nextRetryAt time.Time) error {
	return nil
}

func (m *mockOutboxRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

func TestOutboxAwareRepository_Save_UsesOutboxAndMarksCommitted(t *testing.T) {
	ctx := context.Background()

	baseStore := store.NewMemoryEventStore()
	mox := &mockOutboxRepo{}

	storeWithOutbox, err := NewDomainEventStore(DomainEventStoreOptions[*outboxAggregate]{
		AggregateType: "OutboxAggregate",
		Factory:       newOutboxAggregate,
		EventStore:    baseStore,
		OutboxRepo:    mox,
	})
	require.NoError(t, err)

	repo, err := deventsourced.NewEventSourcedRepository("OutboxAggregate", newOutboxAggregate, storeWithOutbox)
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
