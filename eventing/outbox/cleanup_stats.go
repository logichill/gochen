package outbox

import (
	"context"
	"time"

	"gochen/errors"
)

// countOldRecords 统计旧记录数量。
func (s *CleanupService) countOldRecords(ctx context.Context, olderThan time.Time) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM event_outbox
		WHERE status = ? AND published_at < ?
	`

	var count int64
	err := s.db.QueryRow(ctx, query, OutboxStatusPublished, olderThan).Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, errors.Dependency, "count old records failed")
	}
	return count, nil
}

func (s *CleanupService) Statistics(ctx context.Context) (*OutboxStatistics, error) {
	query := `
		SELECT 
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END) as published,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
			MIN(created_at) as oldest,
			MAX(created_at) as newest
		FROM event_outbox
	`

	var stats OutboxStatistics
	var oldest, newest *time.Time
	err := s.db.QueryRow(ctx, query).Scan(&stats.PendingCount, &stats.PublishedCount, &stats.FailedCount, &oldest, &newest)
	if err != nil {
		return nil, errors.Wrap(err, errors.Dependency, "get outbox statistics failed")
	}
	if oldest != nil {
		stats.OldestCreatedAt = *oldest
	}
	if newest != nil {
		stats.NewestCreatedAt = *newest
	}
	return &stats, nil
}

// OutboxStatistics 定义发件箱统计信息。
type OutboxStatistics struct {
	PendingCount    int64     `json:"pending_count"`
	PublishedCount  int64     `json:"published_count"`
	FailedCount     int64     `json:"failed_count"`
	OldestCreatedAt time.Time `json:"oldest_created_at"`
	NewestCreatedAt time.Time `json:"newest_created_at"`
}
