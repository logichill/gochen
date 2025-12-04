package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gochen/data/db"
	"gochen/eventing"
	"gochen/eventing/monitoring"
	log "gochen/logging"
)

func (s *SQLEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return &eventing.EventStoreError{Code: eventing.ErrStoreFailed.Code, Message: "begin transaction failed", Cause: err}
	}
	defer tx.Rollback()
	if err := s.AppendEventsWithDB(ctx, tx, aggregateID, events, expectedVersion); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return &eventing.EventStoreError{Code: eventing.ErrStoreFailed.Code, Message: "commit transaction failed", Cause: err}
	}
	log.GetLogger().Info(ctx, "events appended", log.Int64("aggregate_id", aggregateID), log.Int("event_count", len(events)))
	return nil
}

func (s *SQLEventStore) AppendEventsWithDB(ctx context.Context, db db.IDatabase, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error {
	if len(events) == 0 {
		return nil
	}
	aggregateType := ""
	for _, evt := range events {
		if evt.GetAggregateType() != "" {
			aggregateType = evt.GetAggregateType()
			break
		}
	}
	currentVersion, err := s.getCurrentVersion(ctx, db, aggregateID, aggregateType)
	if err != nil {
		return &eventing.EventStoreError{Code: eventing.ErrStoreFailed.Code, Message: "query current version failed", Cause: err}
	}
	if currentVersion != expectedVersion {
		return eventing.NewConcurrencyError(aggregateID, expectedVersion, currentVersion)
	}

	insertSQL := fmt.Sprintf(`INSERT INTO %s (id, type, aggregate_id, aggregate_type, version, schema_version, timestamp, payload, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.tableName)
	start := time.Now()
	for idx := range events {
		evt := events[idx] // 直接使用，不取地址
		if evt.GetAggregateType() == "" {
			evt.SetAggregateType(aggregateType)
		} else if aggregateType == "" {
			aggregateType = evt.GetAggregateType()
		} else if evt.GetAggregateType() != aggregateType {
			return &eventing.EventStoreError{
				Code:      eventing.ErrInvalidEvent.Code,
				Message:   "mixed aggregate types in append batch",
				EventID:   evt.GetID(),
				EventType: evt.GetType(),
			}
		}
		expectedEventVersion := expectedVersion + uint64(idx) + 1
		if evt.GetVersion() != expectedEventVersion {
			return &eventing.EventStoreError{
				Code:      eventing.ErrInvalidEvent.Code,
				Message:   fmt.Sprintf("event version mismatch: expected %d, got %d", expectedEventVersion, evt.GetVersion()),
				EventID:   evt.GetID(),
				EventType: evt.GetType(),
			}
		}
		if err := evt.Validate(); err != nil {
			return &eventing.EventStoreError{Code: eventing.ErrInvalidEvent.Code, Message: "event validation failed", Cause: err, EventID: evt.GetID(), EventType: evt.GetType()}
		}
		payloadJSON, err := json.Marshal(evt.GetPayload())
		if err != nil {
			return &eventing.EventStoreError{Code: "SERIALIZE_PAYLOAD_FAILED", Message: "serialize payload failed", Cause: err, EventID: evt.GetID(), EventType: evt.GetType()}
		}
		metadataJSON, err := json.Marshal(evt.GetMetadata())
		if err != nil {
			return &eventing.EventStoreError{Code: "SERIALIZE_METADATA_FAILED", Message: "serialize metadata failed", Cause: err, EventID: evt.GetID(), EventType: evt.GetType()}
		}
		_, err = db.Exec(ctx, insertSQL, evt.GetID(), evt.GetType(), aggregateID, evt.GetAggregateType(), evt.GetVersion(), evt.GetSchemaVersion(), evt.GetTimestamp(), string(payloadJSON), string(metadataJSON))
		if err != nil {
			if isDuplicateKeyError(err) {
				if s.isSameEvent(ctx, db, evt.GetID(), evt.GetVersion(), aggregateID) {
					continue
				}
				return eventing.NewEventAlreadyExistsError(evt.GetID(), aggregateID, evt.GetVersion())
			}
			return &eventing.EventStoreError{Code: eventing.ErrStoreFailed.Code, Message: "insert event failed", Cause: err, EventID: evt.GetID(), EventType: evt.GetType()}
		}
	}
	duration := time.Since(start)
	monitoring.GlobalMetrics().RecordEventSaved(len(events), duration)
	log.GetLogger().Debug(ctx, "append batch done", log.Int64("aggregate_id", aggregateID), log.Int("written", len(events)), log.Int64("ms", duration.Milliseconds()))
	return nil
}

func (s *SQLEventStore) getCurrentVersion(ctx context.Context, db db.IDatabase, aggregateID int64, aggregateType string) (uint64, error) {
	var current uint64
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(MAX(version), 0) FROM %s WHERE aggregate_id = ? AND aggregate_type = ?", s.tableName), aggregateID, aggregateType)
	if err := row.Scan(&current); err != nil {
		return 0, err
	}
	return current, nil
}

func (s *SQLEventStore) isSameEvent(ctx context.Context, db db.IDatabase, eventID string, version uint64, aggregateID int64) bool {
	var existingVersion uint64
	var existingAggregateID int64
	row := db.QueryRow(ctx, fmt.Sprintf("SELECT aggregate_id, version FROM %s WHERE id = ?", s.tableName), eventID)
	return row.Scan(&existingAggregateID, &existingVersion) == nil && existingVersion == version && existingAggregateID == aggregateID
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "UNIQUE constraint failed")
}
