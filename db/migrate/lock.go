package migrate

import (
	"context"
	"fmt"
	"time"

	"gochen/contextx"
	"gochen/db"
	goerrors "gochen/errors"
)

type lockHeartbeatFailure struct {
	err error
}

func (r *Runner) acquireLock(ctx context.Context) error {
	if r.lockTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.lockTimeout)
		defer cancel()
	}
	token := r.nextLockToken()
	deadline := time.Now().Add(r.lockTimeout)
	for {
		acquired, err := r.tryAcquireLock(ctx, token)
		if err == nil && acquired {
			r.lockToken = token
			r.lockErr.Store(nil)
			r.startLockHeartbeat()
			return nil
		}
		if err != nil && !r.isLockConflict(err) {
			return err
		}
		if waitErr := waitForLockRetry(ctx, deadline, r.lockTimeout); waitErr != nil {
			return waitErr
		}
	}
}

func (r *Runner) tryAcquireLock(ctx context.Context, token uint64) (bool, error) {
	now := r.now().UTC()
	insert := fmt.Sprintf(
		"INSERT INTO %s (migration_type, version, dirty, updated_at) VALUES (?, ?, ?, ?)",
		r.quotedStateTable(),
	)
	if _, err := r.db.Exec(ctx, insert, lockMigrationType, token, 0, now); err == nil {
		return true, nil
	} else if !r.isLockConflict(err) {
		return false, goerrors.Wrap(err, goerrors.Database, "acquire migration lock failed").
			WithContext("table", r.stateTable)
	}
	if r.lockStaleAfter <= 0 {
		return false, ErrLocked
	}

	update := fmt.Sprintf(
		"UPDATE %s SET version = ?, dirty = ?, updated_at = ? WHERE migration_type = ? AND updated_at <= ?",
		r.quotedStateTable(),
	)
	result, err := r.db.Exec(ctx, update, token, 0, now, lockMigrationType, now.Add(-r.lockStaleAfter))
	if err != nil {
		return false, goerrors.Wrap(err, goerrors.Database, "reclaim stale migration lock failed").
			WithContext("table", r.stateTable)
	}
	if rowsAffected(result) == 0 {
		return false, ErrLocked
	}
	return true, nil
}

func (r *Runner) releaseLock() {
	if r.lockToken == 0 {
		return
	}
	r.stopLockHeartbeat()
	token := r.lockToken
	r.lockToken = 0
	query := fmt.Sprintf("DELETE FROM %s WHERE migration_type = ? AND version = ?", r.quotedStateTable())
	_, _ = r.db.Exec(contextx.Background(), query, lockMigrationType, token)
}

func (r *Runner) startLockHeartbeat() {
	interval := lockHeartbeatInterval(r.lockStaleAfter)
	if interval <= 0 || r.lockToken == 0 {
		return
	}
	r.lockErr.Store(nil)
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	r.lockStop = stopCh
	r.lockDone = doneCh
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(doneCh)
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				if err := r.refreshLockWithDB(contextx.Background(), r.db); err != nil {
					r.lockErr.Store(&lockHeartbeatFailure{err: err})
					return
				}
			}
		}
	}()
}

func (r *Runner) stopLockHeartbeat() {
	stopCh := r.lockStop
	doneCh := r.lockDone
	r.lockStop = nil
	r.lockDone = nil
	if stopCh == nil {
		return
	}
	close(stopCh)
	if doneCh == nil {
		return
	}
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
	}
}

func (r *Runner) refreshLock(ctx context.Context) error {
	if err := r.lockHeartbeatError(); err != nil {
		return err
	}
	return r.refreshLockWithDB(ctx, r.db)
}

func (r *Runner) refreshLockWithDB(ctx context.Context, database db.IDatabase) error {
	if r.lockToken == 0 {
		return nil
	}
	if err := r.lockHeartbeatError(); err != nil {
		return err
	}
	query := fmt.Sprintf("UPDATE %s SET updated_at = ? WHERE migration_type = ? AND version = ?", r.quotedStateTable())
	result, err := database.Exec(ctx, query, r.now().UTC(), lockMigrationType, r.lockToken)
	if err != nil {
		return goerrors.Wrap(err, goerrors.Database, "refresh migration lock failed").
			WithContext("table", r.stateTable)
	}
	if rowsAffected(result) == 0 {
		return ErrLocked
	}
	return nil
}

func (r *Runner) lockHeartbeatError() error {
	if r == nil {
		return nil
	}
	failure := r.lockErr.Load()
	if failure == nil || failure.err == nil {
		return nil
	}
	return failure.err
}

func (r *Runner) nextLockToken() uint64 {
	token := uint64(r.now().UnixNano())
	if token == 0 {
		return 1
	}
	return token
}

func (r *Runner) isLockConflict(err error) bool {
	return err != nil && (goerrors.Is(err, ErrLocked) || r.dialect.IsUniqueViolation(err))
}

func lockHeartbeatInterval(staleAfter time.Duration) time.Duration {
	if staleAfter <= 0 {
		return 0
	}
	interval := staleAfter / 3
	if interval <= 0 {
		return 0
	}
	if interval < 10*time.Millisecond {
		return 10 * time.Millisecond
	}
	return interval
}
