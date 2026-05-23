package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing"
	"gochen/logging"
)

// TestSQLOutboxRepository_SaveWithEvents 验证 SQLOutboxRepository SaveWithEvents。
func TestSQLOutboxRepository_SaveWithEvents(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	aggregateID := int64(123)

	events := []eventing.Event[int64]{
		newTestEvent(aggregateID, 1, "event-1", map[string]any{"value": 100}),
		newTestEvent(aggregateID, 2, "event-2", map[string]any{"value": 200}),
	}

	err = repo.SaveWithEvents(ctx, aggregateID, events)
	assert.NoError(t, err)

	assert.Len(t, eventStore.events, 2)
	assert.Equal(t, "event-1", eventStore.events[0].GetID())
	assert.Equal(t, "event-2", eventStore.events[1].GetID())

	entries, err := repo.ClaimPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	entry1 := entries[0]
	assert.Equal(t, aggregateID, entry1.AggregateID)
	assert.Equal(t, "TestAggregate", entry1.AggregateType)
	assert.Equal(t, "event-1", entry1.EventID)
	assert.Equal(t, "TestEvent", entry1.EventType)
	assert.Equal(t, OutboxStatusProcessing, entry1.Status)
	assert.NotEmpty(t, entry1.ClaimToken)
	assert.Equal(t, 0, entry1.RetryCount)
	assert.Nil(t, entry1.PublishedAt)
	assert.NotNil(t, entry1.LeaseUntil)
	assert.Nil(t, entry1.NextRetryAt)
	assert.NotEmpty(t, entry1.EventData)
}

// TestSQLOutboxRepository_ClaimPendingEntries 验证 SQLOutboxRepository ClaimPendingEntries。
func TestSQLOutboxRepository_ClaimPendingEntries(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()

	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
		newTestEvent(2, 1, "event-2", nil),
	}

	err = repo.SaveWithEvents(ctx, 1, events[:1])
	assert.NoError(t, err)

	err = repo.SaveWithEvents(ctx, 2, events[1:])
	assert.NoError(t, err)

	entries, err := repo.ClaimPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	assert.Equal(t, "event-1", entries[0].EventID)
	assert.Equal(t, "event-2", entries[1].EventID)

	entries, err = repo.ClaimPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 0)
}

func TestSQLOutboxRepository_ClaimPendingEntries_ReclaimsExpiredProcessingLease(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	_, err = database.Exec(ctx,
		`UPDATE event_outbox SET lease_until = ? WHERE id = ?`,
		time.Now().Add(-time.Minute),
		entries[0].ID,
	)
	require.NoError(t, err)

	entries, err = repo.ClaimPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "event-1", entries[0].EventID)
	assert.Equal(t, OutboxStatusProcessing, entries[0].Status)
	assert.NotEmpty(t, entries[0].ClaimToken)
}

func TestSQLOutboxRepository_ClaimPendingEntries_UsesConfiguredClaimLease(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	cfg := DefaultOutboxConfig()
	cfg.ClaimLease = 30 * time.Second
	repo, err := NewSimpleSQLOutboxRepositoryWithConfig(database, eventStore, logging.NewNoopLogger(), cfg)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	before := time.Now()
	entries, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].LeaseUntil)
	require.True(t, entries[0].LeaseUntil.After(before.Add(20*time.Second)))
	require.True(t, entries[0].LeaseUntil.Before(before.Add(40*time.Second)))
}

func TestSQLOutboxRepository_ClaimPendingEntries_DoesNotUpdateUnselectedEligibleRows(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-pending", nil),
		newTestEvent(2, 1, "event-failed", nil),
		newTestEvent(3, 1, "event-expired-processing", nil),
	}))

	base := time.Now().Add(-10 * time.Minute)
	_, err = database.Exec(ctx, `
		UPDATE event_outbox
		SET created_at = CASE event_id
				WHEN 'event-pending' THEN ?
				WHEN 'event-failed' THEN ?
				WHEN 'event-expired-processing' THEN ?
			END,
			status = CASE event_id
				WHEN 'event-pending' THEN 'pending'
				WHEN 'event-failed' THEN 'failed'
				WHEN 'event-expired-processing' THEN 'processing'
			END,
			claim_token = CASE event_id
				WHEN 'event-expired-processing' THEN 'expired-token'
				ELSE ''
			END,
			lease_until = CASE event_id
				WHEN 'event-expired-processing' THEN ?
				ELSE NULL
			END,
			next_retry_at = CASE event_id
				WHEN 'event-failed' THEN ?
				ELSE NULL
			END
	`, base, base.Add(time.Minute), base.Add(2*time.Minute), base.Add(-time.Minute), base.Add(-time.Minute))
	require.NoError(t, err)

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "event-pending", entries[0].EventID)

	var failedStatus, failedToken string
	row := database.QueryRow(ctx, `SELECT status, claim_token FROM event_outbox WHERE event_id = ?`, "event-failed")
	require.NoError(t, row.Scan(&failedStatus, &failedToken))
	require.Equal(t, string(OutboxStatusFailed), failedStatus)
	require.Empty(t, failedToken)

	var expiredStatus, expiredToken string
	row = database.QueryRow(ctx, `SELECT status, claim_token FROM event_outbox WHERE event_id = ?`, "event-expired-processing")
	require.NoError(t, row.Scan(&expiredStatus, &expiredToken))
	require.Equal(t, string(OutboxStatusProcessing), expiredStatus)
	require.Equal(t, "expired-token", expiredToken)
}

func TestBatchClaimedEntriesWhere_WrapsDisjunction(t *testing.T) {
	entries := []ClaimedEntry{
		{ID: 1, ClaimToken: "claim-a"},
		{ID: 2, ClaimToken: "claim-b"},
	}

	require.Equal(t, "((id = ? AND claim_token = ?) OR (id = ? AND claim_token = ?))", claimedEntriesWhere(entries))
	require.Equal(t, []any{int64(1), "claim-a", int64(2), "claim-b"}, claimedEntriesArgs(entries))
}

// TestSQLOutboxRepository_MarkAsPublished 验证 SQLOutboxRepository MarkAsPublished。
func TestSQLOutboxRepository_MarkAsPublished(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()

	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}

	err = repo.SaveWithEvents(ctx, 1, events)
	assert.NoError(t, err)

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	entryID := entries[0].ID

	err = repo.MarkAsPublished(ctx, entryID, entries[0].ClaimToken)
	assert.NoError(t, err)

	entries, err = repo.ClaimPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 0)
}

// TestSQLOutboxRepository_MarkAsFailed 验证 SQLOutboxRepository MarkAsFailed。
func TestSQLOutboxRepository_MarkAsFailed(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()

	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}

	err = repo.SaveWithEvents(ctx, 1, events)
	assert.NoError(t, err)

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	entryID := entries[0].ID
	errorMsg := "test error"
	nextRetryAt := time.Now().Add(time.Minute)

	err = repo.MarkAsFailed(ctx, entryID, entries[0].ClaimToken, errorMsg, nextRetryAt)
	assert.NoError(t, err)

	entries, err = repo.ClaimPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 0)

	pastRetry := time.Now().Add(-time.Minute)
	_, err = database.Exec(ctx, `UPDATE event_outbox SET next_retry_at = ? WHERE id = ?`, pastRetry, entryID)
	require.NoError(t, err)

	entries, err = repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	err = repo.MarkAsFailed(ctx, entryID, entries[0].ClaimToken, errorMsg, pastRetry)
	assert.NoError(t, err)

	row := database.QueryRow(ctx, `SELECT status, last_error, retry_count FROM event_outbox WHERE id = ?`, entryID)
	var status string
	var retryCount int
	require.NoError(t, row.Scan(&status, &errorMsg, &retryCount))
	assert.Equal(t, string(OutboxStatusFailed), status)
	assert.Equal(t, 2, retryCount)

	entries, err = repo.ClaimPendingEntries(ctx, 10)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, OutboxStatusProcessing, entry.Status)
	assert.Equal(t, errorMsg, entry.LastError)
	assert.Equal(t, 2, entry.RetryCount)
	assert.NotNil(t, entry.LeaseUntil)
	assert.Nil(t, entry.NextRetryAt)
}

func TestSQLOutboxRepository_RenewClaim(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	beforeLease := *entries[0].LeaseUntil
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, repo.RenewClaim(ctx, entries[0].ID, entries[0].ClaimToken))

	row := database.QueryRow(ctx, `SELECT lease_until FROM event_outbox WHERE id = ?`, entries[0].ID)
	var renewedLease time.Time
	require.NoError(t, row.Scan(&renewedLease))
	assert.True(t, renewedLease.After(beforeLease))
}

func TestSQLOutboxRepository_RenewClaim_RequiresCurrentClaimToken(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	err = repo.RenewClaim(ctx, entries[0].ID, "stale-claim")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Conflict))
}

func TestSQLOutboxRepository_MarkAsPublished_RequiresClaimedProcessingEntry(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	row := database.QueryRow(ctx, `SELECT id FROM event_outbox WHERE event_id = ?`, "event-1")
	var entryID int64
	require.NoError(t, row.Scan(&entryID))

	err = repo.MarkAsPublished(ctx, entryID, "stale-claim")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Conflict))
}

func TestSQLOutboxRepository_MarkAsPublished_RejectsStaleClaimTokenAfterReclaim(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, repo.SaveWithEvents(ctx, 1, []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}))

	firstClaim, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, firstClaim, 1)

	_, err = database.Exec(ctx,
		`UPDATE event_outbox SET lease_until = ? WHERE id = ?`,
		time.Now().Add(-time.Minute),
		firstClaim[0].ID,
	)
	require.NoError(t, err)

	secondClaim, err := repo.ClaimPendingEntries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, secondClaim, 1)
	require.NotEqual(t, firstClaim[0].ClaimToken, secondClaim[0].ClaimToken)

	err = repo.MarkAsPublished(ctx, firstClaim[0].ID, firstClaim[0].ClaimToken)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Conflict))

	err = repo.MarkAsPublished(ctx, secondClaim[0].ID, secondClaim[0].ClaimToken)
	require.NoError(t, err)
}

// TestSQLOutboxRepository_DeletePublished 验证 SQLOutboxRepository DeletePublished。
func TestSQLOutboxRepository_DeletePublished(t *testing.T) {
	database := setupTestDB(t)
	eventStore := &MockEventStoreWithDB{}
	repo, err := NewSimpleSQLOutboxRepository(database, eventStore, logging.NewNoopLogger())
	require.NoError(t, err)

	ctx := context.Background()

	events := []eventing.Event[int64]{
		newTestEvent(1, 1, "event-1", nil),
	}

	err = repo.SaveWithEvents(ctx, 1, events)
	assert.NoError(t, err)

	entries, err := repo.ClaimPendingEntries(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	err = repo.MarkAsPublished(ctx, entries[0].ID, entries[0].ClaimToken)
	assert.NoError(t, err)

	olderThan := time.Now().Add(time.Hour)
	err = repo.DeletePublished(ctx, olderThan)
	assert.NoError(t, err)

	var count int64
	row := database.QueryRow(ctx, `SELECT COUNT(1) FROM event_outbox`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count scan error: %v", err)
	}
	assert.Equal(t, int64(0), count)
}
