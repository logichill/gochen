package outbox

import (
	"context"
	"time"

	"gochen/db/dialect"
	"gochen/errors"
)

// deleteOldRecords 分批删除早于阈值的已发布记录。
func (s *CleanupService) deleteOldRecords(ctx context.Context, olderThan time.Time) (int64, error) {
	if s.policy.DryRun {
		return s.countOldRecords(ctx, olderThan)
	}

	var totalDeleted int64
	sleepTimer := time.NewTimer(0)
	if !sleepTimer.Stop() {
		select {
		case <-sleepTimer.C:
		default:
		}
	}
	defer sleepTimer.Stop()

	for {
		sqlQuery, args := s.buildDeleteBatchQuery(olderThan, int64(s.policy.BatchSize))

		rawResult, err := s.db.Exec(ctx, sqlQuery, args...)
		if err != nil {
			return totalDeleted, errors.Wrap(err, errors.Dependency, "delete old records failed")
		}

		affected, err := rawResult.RowsAffected()
		if err != nil {
			return totalDeleted, errors.Wrap(err, errors.Dependency, "get rows affected failed")
		}

		totalDeleted += affected
		if affected < int64(s.policy.BatchSize) {
			break
		}

		if !sleepTimer.Stop() {
			select {
			case <-sleepTimer.C:
			default:
			}
		}
		sleepTimer.Reset(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		case <-sleepTimer.C:
		}
	}

	return totalDeleted, nil
}

func (s *CleanupService) buildDeleteBatchQuery(olderThan time.Time, limit int64) (string, []any) {
	if s.dialect.Name() == dialect.NameMySQL {
		return `
			DELETE FROM event_outbox
			WHERE status = ? AND published_at < ?
			LIMIT ?
		`, []any{OutboxStatusPublished, olderThan, limit}
	}

	return `
		DELETE FROM event_outbox
		WHERE id IN (
			SELECT id FROM event_outbox
			WHERE status = ? AND published_at < ?
			ORDER BY published_at
			LIMIT ?
		)
	`, []any{OutboxStatusPublished, olderThan, limit}
}
