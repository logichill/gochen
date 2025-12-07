package sql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	estore "gochen/eventing/store"
)

// Stream 基于选项的游标化流式读取（可选能力）
func (s *SQLEventStore) Stream(ctx context.Context, opts estore.StreamOptions) (estore.StreamResult[int64], error) {
	res, err := s.GetEventStreamWithCursor(ctx, &opts)
	if err != nil {
		return estore.StreamResult[int64]{}, err
	}
	return *res, nil
}

// GetEventStreamWithCursor 获取基于游标的事件流（兼容旧接口）
func (s *SQLEventStore) GetEventStreamWithCursor(ctx context.Context, opts *estore.StreamOptions) (*estore.StreamResult[int64], error) {
	if opts == nil {
		opts = &estore.StreamOptions{}
	}

	var cursorTimestamp time.Time
	if opts.After != "" {
		row := s.db.QueryRow(ctx, fmt.Sprintf("SELECT timestamp FROM %s WHERE id = ?", s.tableName), opts.After)
		if err := row.Scan(&cursorTimestamp); err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("sqleventstore: cursor %q not found", opts.After)
			}
			return nil, err
		}
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "SELECT id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata FROM %s WHERE 1=1", s.tableName)
	args := make([]any, 0, 10)

	if !opts.FromTime.IsZero() {
		builder.WriteString(" AND timestamp >= ?")
		args = append(args, opts.FromTime)
	}
	if !opts.ToTime.IsZero() {
		builder.WriteString(" AND timestamp <= ?")
		args = append(args, opts.ToTime)
	}
	if len(opts.Types) > 0 {
		builder.WriteString(" AND type IN (" + placeholders(len(opts.Types)) + ")")
		for _, t := range opts.Types {
			args = append(args, t)
		}
	}
	if len(opts.AggregateTypes) > 0 {
		builder.WriteString(" AND aggregate_type IN (" + placeholders(len(opts.AggregateTypes)) + ")")
		for _, t := range opts.AggregateTypes {
			args = append(args, t)
		}
	}
	if opts.After != "" && !cursorTimestamp.IsZero() {
		builder.WriteString(" AND (timestamp > ? OR (timestamp = ? AND id > ?))")
		args = append(args, cursorTimestamp, cursorTimestamp, opts.After)
	}

	builder.WriteString(" ORDER BY timestamp ASC, id ASC")
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}
	queryLimit := limit + 1
	fmt.Fprintf(&builder, " LIMIT %d", queryLimit)

	rows, err := s.db.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events, err := s.scanEvents(rows)
	if err != nil {
		return nil, err
	}

	result := &estore.StreamResult[int64]{
		Events: events,
	}
	if len(events) == queryLimit {
		result.HasMore = true
		result.Events = events[:limit]
	}
	if len(result.Events) > 0 {
		last := result.Events[len(result.Events)-1]
		result.NextCursor = last.GetID()
	}

	return result, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	sb := strings.Builder{}
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
	}
	return sb.String()
}
