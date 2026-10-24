package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/process/task"

	"gochen/eventing"
)

type recoverMarkFailureRepo struct {
	failedEntryID int64
	claimToken    string
	errorMsg      string
	nextRetryAt   time.Time
	markFailedErr error
	ctxErr        error
}

func (r *recoverMarkFailureRepo) SaveWithEvents(ctx context.Context, aggregateID int64, events []eventing.Event[int64]) error {
	return nil
}

func (r *recoverMarkFailureRepo) ClaimPendingEntries(ctx context.Context, limit int) ([]OutboxEntry[int64], error) {
	return nil, nil
}

func (r *recoverMarkFailureRepo) MarkAsPublished(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

func (r *recoverMarkFailureRepo) MarkAsFailed(ctx context.Context, entryID int64, claimToken string, errorMsg string, nextRetryAt time.Time) error {
	r.ctxErr = ctx.Err()
	if r.ctxErr != nil {
		return r.ctxErr
	}
	if r.markFailedErr != nil {
		return r.markFailedErr
	}
	r.failedEntryID = entryID
	r.claimToken = claimToken
	r.errorMsg = errorMsg
	r.nextRetryAt = nextRetryAt
	return nil
}

func (r *recoverMarkFailureRepo) RenewClaim(ctx context.Context, entryID int64, claimToken string) error {
	return nil
}

func (r *recoverMarkFailureRepo) DeletePublished(ctx context.Context, olderThan time.Time) error {
	return nil
}

// TestOutboxPublisherCoreRecoverPublishedMarkFailureMarksEntryFailed 锁定有界重试语义：
// 发布成功但 MarkAsPublished 持续失败时，必须把 mark 失败计入 RetryCount（MarkAsFailed），
// 否则条目以 PROCESSING 滞留到租约到期被无界重复投递且永不进入终态。
func TestOutboxPublisherCoreRecoverPublishedMarkFailureMarksEntryFailed(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	core := outboxPublisherCore[int64]{repo: repo, cfg: OutboxConfig{RetryInterval: time.Second}}
	markErr := errors.New("mark published failed")

	err := core.recoverPublishedMarkFailure(context.Background(), OutboxEntry[int64]{
		ID:         42,
		RetryCount: 1,
		ClaimToken: "claim-42",
	}, markErr)

	require.Error(t, err)
	require.ErrorIs(t, err, markErr)
	require.Equal(t, int64(42), repo.failedEntryID)
	require.Equal(t, "claim-42", repo.claimToken)
	// errorMsg 必须注明事件已投递，避免 DLQ/排障时误判为未消费。
	require.Contains(t, repo.errorMsg, "event was published")
	require.False(t, repo.nextRetryAt.IsZero())
}

// TestOutboxPublisherCoreRecoverPublishedMarkFailureMovesToDLQWhenRetriesExhausted 锁定
// DLQ 逃生：mark 失败计入 RetryCount 后达到上限时，必须迁入死信队列而不是无限重投。
func TestOutboxPublisherCoreRecoverPublishedMarkFailureMovesToDLQWhenRetriesExhausted(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	dlq := &MockDLQRepository{}
	core := outboxPublisherCore[int64]{
		repo: repo,
		cfg:  OutboxConfig{RetryInterval: time.Second, MaxRetries: 3},
		dlq:  dlq,
	}
	markErr := errors.New("mark published failed")

	err := core.recoverPublishedMarkFailure(context.Background(), OutboxEntry[int64]{
		ID:         42,
		RetryCount: 2, // mark 失败计入后 RetryCount+1 == MaxRetries
		ClaimToken: "claim-42",
	}, markErr)

	require.Error(t, err)
	require.ErrorIs(t, err, markErr)
	require.Equal(t, int64(42), repo.failedEntryID)
	moved := dlq.MovedEntries()
	require.Len(t, moved, 1)
	require.Equal(t, int64(42), moved[0].ID)
	require.Equal(t, 3, moved[0].RetryCount)
	require.Contains(t, moved[0].LastError, "event was published")
}

// TestOutboxPublisherCoreRecoverPublishedMarkFailureNoMaxRetries_NeverMovesToDLQ 锁定
// NoMaxRetries(-1) 语义：mark 失败仍计入 RetryCount，但永不迁入死信队列（无限重试）。
func TestOutboxPublisherCoreRecoverPublishedMarkFailureNoMaxRetries_NeverMovesToDLQ(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	dlq := &MockDLQRepository{}
	core := outboxPublisherCore[int64]{
		repo: repo,
		cfg:  OutboxConfig{RetryInterval: time.Second, MaxRetries: NoMaxRetries},
		dlq:  dlq,
	}

	err := core.recoverPublishedMarkFailure(context.Background(), OutboxEntry[int64]{
		ID:         42,
		RetryCount: 99,
		ClaimToken: "claim-42",
	}, errors.New("mark published failed"))

	require.Error(t, err)
	require.Equal(t, int64(42), repo.failedEntryID)
	require.Empty(t, dlq.MovedEntries())
}

func TestOutboxPublisherCoreRecoverPublishedMarkFailureNoopsWithoutError(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	core := outboxPublisherCore[int64]{repo: repo}

	err := core.recoverPublishedMarkFailure(context.Background(), OutboxEntry[int64]{
		ID:         42,
		ClaimToken: "claim-42",
	}, nil)

	require.NoError(t, err)
	require.Zero(t, repo.failedEntryID)
}

func TestOutboxPublisherCoreProcessClaimedFinalizesFailureAfterCallerCancellation(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	core := outboxPublisherCore[int64]{
		repo: repo,
		bus:  &MockEventBus{},
		cfg:  OutboxConfig{RetryInterval: time.Second},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := core.processClaimed(ctx, OutboxEntry[int64]{
		ID:         42,
		ClaimToken: "claim-42",
	}, outboxMarkStrategy[int64]{markFailed: core.markFailureDirect})

	require.NoError(t, err)
	require.NoError(t, repo.ctxErr)
	require.Equal(t, int64(42), repo.failedEntryID)
	require.Equal(t, "claim-42", repo.claimToken)
}

func TestRunWithClaimKeepaliveReturnsRenewErrorEvenWhenRunSucceeds(t *testing.T) {
	renewErr := errors.New("lease lost")
	renewCalled := make(chan struct{})
	repo := &MockOutboxRepository{
		renewClaimFunc: func(context.Context, int64, string) error {
			close(renewCalled)
			return renewErr
		},
	}
	entry := OutboxEntry[int64]{
		ID:         42,
		Status:     OutboxStatusProcessing,
		ClaimToken: "claim-42",
	}
	leaseUntil := time.Now().Add(time.Minute)
	entry.LeaseUntil = &leaseUntil

	supervisor := task.NewTaskSupervisor("eventing.outbox.claim_keepalive.test")
	err := runWithClaimKeepalive(context.Background(), supervisor, repo, entry, time.Hour, time.Millisecond, func(ctx context.Context) error {
		select {
		case <-renewCalled:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	require.Error(t, err)
	var keepaliveErr *claimKeepaliveError
	require.ErrorAs(t, err, &keepaliveErr)
	require.ErrorIs(t, err, renewErr)
}
