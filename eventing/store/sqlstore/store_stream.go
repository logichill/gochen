package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gochen/errors"
	estore "gochen/eventing/store"
)

// StreamEvents 获取基于游标的事件流（游标分页语义）。
func (s *SQLEventStore[ID]) StreamEvents(ctx context.Context, opts *estore.StreamOptions) (*estore.StreamResult[ID], error) {
	if opts == nil {
		opts = &estore.StreamOptions{}
	}

	var cursorTimestamp time.Time
	if opts.After != "" {
		row := s.db.QueryRow(ctx, fmt.Sprintf("SELECT timestamp FROM %s WHERE id = ?", s.tableName), opts.After)
		if err := row.Scan(&cursorTimestamp); err != nil {
			if err == sql.ErrNoRows {
				return nil, errors.NewCode(errors.NotFound, "cursor not found").WithContext("cursor", opts.After)
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
		limit = estore.DefaultStreamLimit
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

	result := &estore.StreamResult[ID]{
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
