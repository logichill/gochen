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
	if run == nil {
		return &claimKeepaliveError{cause: errors.NewCode(errors.InvalidInput, "run func is nil")}
	}
	if !entry.claimIsActive() {
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
	go func() {
		defer close(doneCh)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := repo.RenewClaim(runCtx, entry.ID, entry.ClaimToken); err != nil {
					if stderrors.Is(err, context.Canceled) {
						select {
						case <-runCompletedCh:
							return
						default:
						}
					}
					select {
					case renewErrCh <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()

	err := run(runCtx)
	close(runCompletedCh)
	cancel()
	<-doneCh

	select {
	case renewErr := <-renewErrCh:
		return &claimKeepaliveError{cause: renewErr}
	default:
		return err
	}
}
