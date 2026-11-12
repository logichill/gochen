package outbox

import (
	"context"
	stderrors "errors"
	"time"

	"gochen/errors"
)

type claimKeepaliveError struct {
	cause error
}

type claimJobKey struct{}
type claimJob[ID comparable] struct {
	entry     OutboxEntry[ID]
	interval  time.Duration
	renewErr  chan error
	done      chan struct{}
	completed chan struct{}
	ready     chan error
	cancel    context.CancelFunc
}

func (e *claimKeepaliveError) Error() string {
	if e == nil || e.cause == nil {
		return "outbox claim keepalive failed"
	}
	return e.cause.Error()
}

func (e *claimKeepaliveError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func runWithClaimKeepalive[ID comparable](
	ctx context.Context,
	tasks *publisherTasks[ID],
	repo IOutboxRepository[ID],
	entry OutboxEntry[ID],
	claimLease time.Duration,
	renewInterval time.Duration,
	run func(context.Context) error,
) error {
	if ctx == nil {
		return &claimKeepaliveError{cause: errors.NewCode(errors.InvalidInput, "ctx is nil")}
	}
	if repo == nil {
		return &claimKeepaliveError{cause: errors.NewCode(errors.InvalidInput, "outbox repository is nil")}
	}
	if err := tasks.checkActive(); err != nil {
		return &claimKeepaliveError{cause: err}
	}
	if run == nil {
		return &claimKeepaliveError{cause: errors.NewCode(errors.InvalidInput, "run func is nil")}
	}
	if !entry.canProceedWithClaim() {
		return &claimKeepaliveError{cause: errors.NewCode(errors.Conflict, "outbox claim is expired").
			WithContext("entry_id", entry.ID)}
	}

	if claimLease <= 0 {
		claimLease = defaultClaimLease
	}
	interval := renewInterval
	if interval <= 0 || interval >= claimLease {
		interval = claimLease / 2
	}
	if interval <= 0 {
		interval = time.Second
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	renewErrCh := make(chan error, 1)
	doneCh := make(chan struct{})
	runCompletedCh := make(chan struct{})
	job := claimJob[ID]{entry: entry, interval: interval, renewErr: renewErrCh, done: doneCh, completed: runCompletedCh, ready: make(chan error, 1), cancel: cancel}
	if err := tasks.registry.Go(context.WithValue(runCtx, claimJobKey{}, job), tasks.prefix+"renew_claim"); err != nil {
		return &claimKeepaliveError{cause: errors.Wrap(err, errors.Internal, "start outbox claim keepalive failed")}
	}
	if err := <-job.ready; err != nil {
		cancel()
		<-doneCh
		return &claimKeepaliveError{cause: err}
	}

	err := run(runCtx)
	close(runCompletedCh)
	cancel()
	<-doneCh

	select {
	case renewErr := <-renewErrCh:
		return &claimKeepaliveError{cause: renewErr}
	default:
	}
	if err == nil {
		return nil
	}
	return err
}

func (t *publisherTasks[ID]) renewClaim(ctx context.Context) (err error) {
	job, ok := ctx.Value(claimJobKey{}).(claimJob[ID])
	if !ok {
		return errors.NewCode(errors.InvalidInput, "claim renewal requires a trusted claim job")
	}
	defer close(job.done)
	entered := false
	// 完成通知和取消责任包围授权执行器，拒绝及 panic 同样释放发布调用。
	defer func() {
		if failure := recover(); failure != nil {
			err = errors.NewCode(errors.Internal, "claim renewal task panicked").WithContext("panic", failure)
		}
		if !entered {
			if err == nil {
				err = errors.NewCode(errors.FailedPrecondition, "Outbox executor did not enter the registered claim handler")
			}
			job.ready <- err
		}
		if err != nil {
			select {
			case job.renewErr <- err:
			default:
			}
			job.cancel()
		}
	}()
	return t.execute(ctx, "renew_claim", func(ctx context.Context) error {
		entered = true
		job.ready <- nil
		ticker := time.NewTicker(job.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := t.repo.RenewClaim(ctx, job.entry.ID, job.entry.ClaimToken); err != nil {
					if stderrors.Is(err, context.Canceled) {
						select {
						case <-job.completed:
							return nil
						default:
						}
					}
					return err
				}
			}
		}
	})
}
