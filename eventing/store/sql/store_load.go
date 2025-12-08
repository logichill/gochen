package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

func (s *SQLEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	query := fmt.Sprintf("SELECT id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata FROM %s WHERE aggregate_id = ? AND version > ? ORDER BY version ASC", s.tableName)
	rows, err := s.db.Query(ctx, query, aggregateID, afterVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEvents(rows)
}

func (s *SQLEventStore) StreamEvents(ctx context.Context, from time.Time) ([]eventing.Event[int64], error) {
	query := fmt.Sprintf("SELECT id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata FROM %s WHERE timestamp >= ? ORDER BY timestamp ASC, version ASC", s.tableName)
	rows, err := s.db.Query(ctx, query, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEvents(rows)
}

// LoadEventsByType 按聚合类型加载事件（可选接口）
func (s *SQLEventStore) LoadEventsByType(ctx context.Context, aggregateType string, aggregateID int64, afterVersion uint64) ([]eventing.Event[int64], error) {
	query := fmt.Sprintf("SELECT id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata FROM %s WHERE aggregate_id = ? AND aggregate_type = ? AND version > ? ORDER BY version ASC", s.tableName)
	rows, err := s.db.Query(ctx, query, aggregateID, aggregateType, afterVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEvents(rows)
}

// StreamAggregate 按聚合顺序流式读取事件（实现 IEventStreamStore[int64]，可选能力）
func (s *SQLEventStore) StreamAggregate(ctx context.Context, opts *store.AggregateStreamOptions[int64]) (*store.AggregateStreamResult[int64], error) {
	if opts == nil {
		return nil, fmt.Errorf("AggregateStreamOptions cannot be nil")
	}
	if opts.AggregateID <= 0 {
		return nil, fmt.Errorf("invalid aggregate id %d", opts.AggregateID)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	// 构建查询：按版本升序读取指定聚合的事件，限制条数
	base := fmt.Sprintf("SELECT id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata FROM %s WHERE aggregate_id = ?", s.tableName)
	args := []any{opts.AggregateID}
	if opts.AggregateType != "" {
		base += " AND aggregate_type = ?"
		args = append(args, opts.AggregateType)
	}
	base += " AND version > ? ORDER BY version ASC LIMIT ?"
	args = append(args, opts.AfterVersion, limit+1) // 多取一条判断 HasMore

	rows, err := s.db.Query(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events, err := s.scanEvents(rows)
	if err != nil {
		return nil, err
	}

	res := &store.AggregateStreamResult[int64]{Events: events}
	if len(events) == 0 {
		return res, nil
	}
	// 是否还有更多：若返回数量超过 limit，截断并标记 HasMore
	if len(events) > limit {
		res.HasMore = true
		res.Events = events[:limit]
	}
	last := res.Events[len(res.Events)-1]
	res.NextVersion = last.GetVersion()
	return res, nil
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
}

func (s *SQLEventStore) scanEvents(rows rowScanner) ([]eventing.Event[int64], error) {
	var events []eventing.Event[int64]
	for rows.Next() {
		var (
			id, typ      string
			aggID        int64
			aggType      string
			ver          uint64
			schema       int
			ts           time.Time
			payloadJSON  string
			metadataJSON string
		)
		if err := rows.Scan(&id, &typ, &aggID, &aggType, &ver, &schema, &ts, &payloadJSON, &metadataJSON); err != nil {
			return nil, err
		}

		var payload map[string]any
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event payload for id=%s, type=%s: %w", id, typ, err)
			}
		}

		var metadata map[string]any
		if metadataJSON != "" {
			if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event metadata for id=%s, type=%s: %w", id, typ, err)
			}
		}
		events = append(events, eventing.Event[int64]{
			Message: messaging.Message{
				ID:        id,
				Type:      typ,
				Timestamp: ts,
				Payload:   payload,
				Metadata:  metadata,
			},
			AggregateID:   aggID,
			AggregateType: aggType,
			Version:       ver,
			SchemaVersion: schema,
		})
	}
	return events, nil
}

// HasAggregate 检查聚合是否存在（实现 IAggregateInspector 接口）
func (s *SQLEventStore) HasAggregate(ctx context.Context, aggregateID int64) (bool, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE aggregate_id = ?", s.tableName)
	row := s.db.QueryRow(ctx, query, aggregateID)

	var count int64
	if err := row.Scan(&count); err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetAggregateVersion 获取聚合的当前版本（实现 IAggregateInspector 接口）
func (s *SQLEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	query := fmt.Sprintf("SELECT COALESCE(MAX(version), 0) FROM %s WHERE aggregate_id = ?", s.tableName)
	row := s.db.QueryRow(ctx, query, aggregateID)

	var version uint64
	if err := row.Scan(&version); err != nil {
		return 0, err
	}

	return version, nil
}

// 编译期断言：SQLEventStore 满足事件存储与聚合流接口约束。
var (
	_ store.IEventStore[int64]       = (*SQLEventStore)(nil)
	_ store.IEventStreamStore[int64] = (*SQLEventStore)(nil)
	_ store.IEventStreamStore[int64] = (*SQLEventStore)(nil)
	_ store.IEventStreamStore[int64]    = (*SQLEventStore)(nil)
)
