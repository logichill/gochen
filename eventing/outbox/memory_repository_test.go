package outbox

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
	"gochen/observe/logging"
)

func TestMemoryOutboxRepositorySaveAndDecodeEvent(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(42, 1, "event-1", map[string]any{"value": 7})

	require.NoError(t, repo.SaveWithEvents(context.Background(), 42, []eventing.Event[int64]{evt}))
	entries, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, OutboxStatusProcessing, entries[0].Status)
	require.NotEmpty(t, entries[0].ClaimToken)

	decoded, err := entries[0].ToEventWith(newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)
	require.Equal(t, evt.ID, decoded.ID)
	require.Equal(t, evt.AggregateID, decoded.AggregateID)
	require.Equal(t, evt.AggregateType, decoded.AggregateType)
}

func TestNewMemoryOutboxRepositoryRequiresEventStore(t *testing.T) {
	repo, err := NewMemoryOutboxRepository[int64](nil)
	require.Nil(t, repo)
	require.True(t, errors.Is(err, errors.InvalidInput))

	var typedNil *store.MemoryEventStore[int64]
	repo, err = NewMemoryOutboxRepository[int64](typedNil)
	require.Nil(t, repo)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestMemoryOutboxRepositoryPersistsEventsIntoBoundEventStore(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	repo, err := NewMemoryOutboxRepository[int64](eventStore)
	require.NoError(t, err)
	evt := newTestEvent(42, 1, "event-bound", map[string]any{"value": 7})

	require.NoError(t, repo.SaveWithEvents(context.Background(), 42, []eventing.Event[int64]{evt}))
	loaded, err := eventStore.LoadEvents(context.Background(), "TestAggregate", 42, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, evt.ID, loaded[0].ID)
}

func TestMemoryOutboxRepositoryRecoversOutboxForAlreadyPersistedEvents(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	firstRepo, err := NewMemoryOutboxRepository[int64](eventStore)
	require.NoError(t, err)
	evt := newTestEvent(42, 1, "event-recover", map[string]any{"value": 7})
	require.NoError(t, firstRepo.SaveWithEvents(context.Background(), 42, []eventing.Event[int64]{evt}))

	// 模拟进程重启：事件流仍在，但 Outbox 内存索引从空状态恢复。
	recoveredRepo, err := NewMemoryOutboxRepository[int64](eventStore)
	require.NoError(t, err)
	require.NoError(t, recoveredRepo.SaveWithEvents(context.Background(), 42, []eventing.Event[int64]{evt}))

	loaded, err := eventStore.LoadEvents(context.Background(), "TestAggregate", 42, 0)
	require.NoError(t, err)
	require.Len(t, loaded, 1, "idempotent recovery must not append the event twice")

	claimed, err := recoveredRepo.ClaimPendingEntries(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "recovery must create exactly one outbox entry")
	require.Equal(t, evt.ID, claimed[0].EventID)
}

func TestMemoryOutboxRepositoryEventAndOutboxValidationIsAtomic(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	repo, err := NewMemoryOutboxRepository[int64](eventStore)
	require.NoError(t, err)
	first := newTestEvent(1, 1, "event-atomic", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{first}))

	conflicting := newTestEvent(2, 1, "event-atomic", map[string]any{"changed": true})
	newEvent := newTestEvent(2, 2, "event-new", nil)
	err = repo.SaveWithEvents(context.Background(), 2, []eventing.Event[int64]{conflicting, newEvent})
	require.True(t, errors.Is(err, errors.Duplicate))

	loaded, loadErr := eventStore.LoadEvents(context.Background(), "TestAggregate", 2, 0)
	require.NoError(t, loadErr)
	require.Empty(t, loaded)
	claimed, claimErr := repo.ClaimPendingEntries(context.Background(), 10)
	require.NoError(t, claimErr)
	require.Len(t, claimed, 1)
	require.Equal(t, "event-atomic", claimed[0].EventID)
}

func TestMemoryOutboxRepositorySaveIsAtomic(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	valid := newTestEvent(1, 1, "event-valid", nil)
	invalid := newTestEvent(1, 2, "event-invalid", nil)
	invalid.Payload = messaging.NewPayload(make(chan int))

	err := repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{valid, invalid})
	require.Error(t, err)
	entries, claimErr := repo.ClaimPendingEntries(context.Background(), 10)
	require.NoError(t, claimErr)
	require.Empty(t, entries)
}

func TestMemoryOutboxRepositoryRetriesIdenticalEventIdempotently(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	first := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{first}))
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{first, first}))

	entries, err := repo.ClaimPendingEntries(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestMemoryOutboxRepositoryNormalizesDuplicateAggregateTypeBeforeComparison(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	first := newTestEvent(1, 1, "event-normalized", nil)
	first.AggregateType = " TestAggregate "
	duplicate := first
	duplicate.AggregateType = "TestAggregate"

	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{first, duplicate}))
	require.Equal(t, " TestAggregate ", first.AggregateType, "normalization must not mutate caller event")

	entries, err := repo.ClaimPendingEntries(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "TestAggregate", entries[0].AggregateType)
}

func TestMemoryOutboxRepositoryRejectsConflictingDuplicateEventIDsAtomically(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	first := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{first}))

	duplicate := newTestEvent(2, 1, "event-1", map[string]any{"different": true})
	newEvent := newTestEvent(2, 2, "event-2", nil)
	err := repo.SaveWithEvents(context.Background(), 2, []eventing.Event[int64]{duplicate, newEvent})
	require.True(t, errors.Is(err, errors.Duplicate))

	entries, claimErr := repo.ClaimPendingEntries(context.Background(), 10)
	require.NoError(t, claimErr)
	require.Len(t, entries, 1)
	require.Equal(t, "event-1", entries[0].EventID)
}

func TestMemoryOutboxRepositoryRequiresCurrentClaim(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	claimed, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	require.True(t, errors.Is(repo.MarkAsPublished(context.Background(), claimed[0].ID, "stale"), errors.Conflict))
	require.True(t, errors.Is(repo.MarkAsFailed(context.Background(), claimed[0].ID, "stale", "failed", time.Now()), errors.Conflict))
	require.True(t, errors.Is(repo.RenewClaim(context.Background(), claimed[0].ID, "stale"), errors.Conflict))
	require.NoError(t, repo.MarkAsPublished(context.Background(), claimed[0].ID, claimed[0].ClaimToken))
	require.True(t, errors.Is(repo.MarkAsPublished(context.Background(), claimed[0].ID, claimed[0].ClaimToken), errors.Conflict))
}

func TestMemoryOutboxRepositoryRetriesFailedEntryWhenDue(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	claimed, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	nextRetry := time.Now().Add(time.Hour)
	require.NoError(t, repo.MarkAsFailed(context.Background(), claimed[0].ID, claimed[0].ClaimToken, "failed", nextRetry))

	claimed, err = repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Empty(t, claimed)

	repo.mu.Lock()
	repo.entries[0].NextRetryAt = timePointer(time.Now().Add(-time.Second))
	repo.mu.Unlock()

	claimed, err = repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, 1, claimed[0].RetryCount)
	require.Nil(t, claimed[0].NextRetryAt)
}

func TestMemoryOutboxRepositoryReclaimsExpiredLease(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	firstClaim, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, firstClaim, 1)

	repo.mu.Lock()
	repo.entries[0].LeaseUntil = timePointer(time.Now().Add(-time.Second))
	repo.mu.Unlock()

	secondClaim, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, secondClaim, 1)
	require.NotEqual(t, firstClaim[0].ClaimToken, secondClaim[0].ClaimToken)
	require.True(t, errors.Is(repo.MarkAsPublished(context.Background(), firstClaim[0].ID, firstClaim[0].ClaimToken), errors.Conflict))
	require.NoError(t, repo.RenewClaim(context.Background(), secondClaim[0].ID, secondClaim[0].ClaimToken))
	require.NoError(t, repo.MarkAsPublished(context.Background(), secondClaim[0].ID, secondClaim[0].ClaimToken))
}

func TestMemoryOutboxRepositoryZeroValueUsesDefaultLease(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	repo.claimLease = 0
	evt := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	claimed, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.WithinDuration(t, time.Now().Add(defaultClaimLease), *claimed[0].LeaseUntil, time.Second)

	require.NoError(t, repo.RenewClaim(context.Background(), claimed[0].ID, claimed[0].ClaimToken))
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	require.WithinDuration(t, time.Now().Add(defaultClaimLease), *repo.entries[0].LeaseUntil, time.Second)
}

func TestMemoryOutboxRepositoryDeletePublished(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(1, 1, "event-1", nil)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))
	claimed, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, repo.MarkAsPublished(context.Background(), claimed[0].ID, claimed[0].ClaimToken))

	require.NoError(t, repo.DeletePublished(context.Background(), time.Now().Add(time.Second)))
	require.Empty(t, repo.entries)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))
}

func TestMemoryOutboxRepositoryDeletePublishedClearsRemovedEntries(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(1, 1, "event-with-retained-data", map[string]any{"payload": "retained"})
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))
	claimed, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, repo.MarkAsPublished(context.Background(), claimed[0].ID, claimed[0].ClaimToken))

	require.NoError(t, repo.DeletePublished(context.Background(), time.Now().Add(time.Second)))
	require.Empty(t, repo.entries)
	require.NotZero(t, cap(repo.entries))
	require.Zero(t, repo.entries[:cap(repo.entries)][0], "removed entries must not remain referenced by the backing array")
}

func TestMemoryOutboxRepositoryHonorsContext(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{newTestEvent(1, 1, "event-1", nil)})
	require.ErrorIs(t, err, context.Canceled)
	_, err = repo.ClaimPendingEntries(nil, 1)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestMemoryOutboxRepositoryClaimsEachEntryOnceConcurrently(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	const eventCount = 40
	events := make([]eventing.Event[int64], 0, eventCount)
	for i := 0; i < eventCount; i++ {
		events = append(events, newTestEvent(1, uint64(i+1), eventID(i), nil))
	}
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, events))

	var wg sync.WaitGroup
	claimedIDs := make(chan int64, eventCount)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				entries, err := repo.ClaimPendingEntries(context.Background(), 1)
				require.NoError(t, err)
				if len(entries) == 0 {
					return
				}
				claimedIDs <- entries[0].ID
			}
		}()
	}
	wg.Wait()
	close(claimedIDs)

	seen := make(map[int64]struct{}, eventCount)
	for id := range claimedIDs {
		_, duplicate := seen[id]
		require.False(t, duplicate)
		seen[id] = struct{}{}
	}
	require.Len(t, seen, eventCount)
}

func TestMemoryOutboxRepositoryClaimDoesNotAllocateFromUntrustedLimit(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	entries, err := repo.ClaimPendingEntries(context.Background(), int(^uint(0)>>1))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestMemoryOutboxRepositoryPublishesEndToEnd(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	evt := newTestEvent(1, 1, "event-1", map[string]any{"value": 1})
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	eventBus := &MockEventBus{}
	publisher, err := NewPublisher(repo, eventBus, DefaultOutboxConfig(), logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)
	require.NoError(t, publisher.PublishPending(context.Background()))
	require.Equal(t, 1, eventBus.PublishedEventsLen())

	repo.mu.RLock()
	defer repo.mu.RUnlock()
	require.Len(t, repo.entries, 1)
	require.Equal(t, OutboxStatusPublished, repo.entries[0].Status)
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func eventID(index int) string {
	return "event-" + strconv.Itoa(index)
}

func newMemoryOutboxRepository(t *testing.T) *MemoryOutboxRepository[int64] {
	t.Helper()
	repo, err := NewMemoryOutboxRepository[int64](store.NewMemoryEventStore[int64]())
	require.NoError(t, err)
	return repo
}
