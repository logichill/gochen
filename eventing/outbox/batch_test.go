package outbox

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/logging"
)

func TestBatchOperations_MarkAsFailedBatch_RollsBackOnClaimConflict(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	now := time.Now()
	leaseUntil := now.Add(time.Minute)

	_, err := database.Exec(ctx, `
		INSERT INTO event_outbox (
			id, aggregate_id, aggregate_type, event_id, event_type, event_data,
			status, claim_token, created_at, retry_count, lease_until
		) VALUES
			(1, 1, 'TestAggregate', 'event-1', 'TestEvent', '{}', ?, 'owned-1', ?, 2, ?),
			(2, 2, 'TestAggregate', 'event-2', 'TestEvent', '{}', ?, 'owned-2', ?, 2, ?)
	`, OutboxStatusProcessing, now, leaseUntil, OutboxStatusProcessing, now, leaseUntil)
	require.NoError(t, err)

	batchOps := NewBatchOperations(database)
	err = batchOps.MarkAsFailedBatch(ctx, []FailedEntry{
		{
			ClaimedEntry: ClaimedEntry{ID: 1, ClaimToken: "owned-1"},
			Error:        "current failure",
			NextRetryAt:  now.Add(time.Hour),
		},
		{
			ClaimedEntry: ClaimedEntry{ID: 2, ClaimToken: "stale-token"},
			Error:        "stale failure",
			NextRetryAt:  now.Add(time.Hour),
		},
	})
	require.Error(t, err)

	var status, claimToken string
	var retryCount int
	var lastError sql.NullString
	require.NoError(t, database.QueryRow(ctx, `
		SELECT status, claim_token, retry_count, last_error
		FROM event_outbox
		WHERE id = 1
	`).Scan(&status, &claimToken, &retryCount, &lastError))
	assert.Equal(t, string(OutboxStatusProcessing), status)
	assert.Equal(t, "owned-1", claimToken)
	assert.Equal(t, 2, retryCount)
	assert.False(t, lastError.Valid)

	repo, err := NewSimpleSQLOutboxRepository(database, &MockEventStoreWithDB{}, logging.NewNoopLogger())
	require.NoError(t, err)
	require.NoError(t, repo.MarkAsFailed(ctx, 1, "owned-1", "fallback failure", now.Add(time.Hour)))

	require.NoError(t, database.QueryRow(ctx, `
		SELECT status, claim_token, retry_count, last_error
		FROM event_outbox
		WHERE id = 1
	`).Scan(&status, &claimToken, &retryCount, &lastError))
	assert.Equal(t, string(OutboxStatusFailed), status)
	assert.Empty(t, claimToken)
	assert.Equal(t, 3, retryCount)
	if assert.True(t, lastError.Valid) {
		assert.Equal(t, "fallback failure", lastError.String)
	}
}
