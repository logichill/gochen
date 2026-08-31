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
	require.Equal(t, int64(42), repo.failedEntryID)
	require.Equal(t, "claim-42", repo.claimToken)
	require.Equal(t, markErr.Error(), repo.errorMsg)
	require.False(t, repo.nextRetryAt.IsZero())
}

func TestOutboxPublisherCoreRecoverPublishedMarkFailureIgnoresCallerCancellation(t *testing.T) {
	repo := &recoverMarkFailureRepo{}
	core := outboxPublisherCore[int64]{repo: repo, cfg: OutboxConfig{RetryInterval: time.Second}}
	markErr := errors.New("mark published failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := core.recoverPublishedMarkFailure(ctx, OutboxEntry[int64]{
		ID:         42,
		RetryCount: 1,
		ClaimToken: "claim-42",
	}, markErr)

	require.Error(t, err)
	require.NoError(t, repo.ctxErr)
	require.Equal(t, int64(42), repo.failedEntryID)
	require.Equal(t, "claim-42", repo.claimToken)
}

func TestOutboxPublisherCoreRecoverPublishedMarkFailureReturnsRecoveryError(t *testing.T) {
	repo := &recoverMarkFailureRepo{markFailedErr: errors.New("db down")}
	core := outboxPublisherCore[int64]{repo: repo, cfg: OutboxConfig{RetryInterval: time.Second}}
	markErr := errors.New("mark published failed")

	err := core.recoverPublishedMarkFailure(context.Background(), OutboxEntry[int64]{
		ID:         42,
		ClaimToken: "claim-42",
	}, markErr)

	require.Error(t, err)
	require.ErrorIs(t, err, repo.markFailedErr)
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
