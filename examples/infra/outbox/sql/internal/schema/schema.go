package schema

import (
	"context"
	"fmt"

	dbcore "gochen/storage/database"
)

// EnsureEventAndOutboxTables 创建演示所需的表（SQLite 兼容 DDL）
func EnsureEventAndOutboxTables(ctx context.Context, db dbcore.IDatabase) error {
	// 事件存储表（event_store）
	eventDDL := `
	CREATE TABLE IF NOT EXISTS event_store (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		aggregate_id INTEGER NOT NULL,
		aggregate_type TEXT NOT NULL,
		version INTEGER NOT NULL,
		schema_version INTEGER NOT NULL,
		timestamp DATETIME NOT NULL,
		payload TEXT NOT NULL,
		metadata TEXT NOT NULL,
		UNIQUE(aggregate_id, aggregate_type, version)
	);`
	if _, err := db.Exec(ctx, eventDDL); err != nil {
		return fmt.Errorf("create event_store: %w", err)
	}

	// Outbox 表（event_outbox）
	outboxDDL := `
	CREATE TABLE IF NOT EXISTS event_outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		aggregate_id INTEGER NOT NULL,
		aggregate_type TEXT NOT NULL,
		event_id TEXT NOT NULL UNIQUE,
		event_type TEXT NOT NULL,
		event_data TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		published_at DATETIME NULL,
		retry_count INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NULL,
		next_retry_at DATETIME NULL
	);`
	if _, err := db.Exec(ctx, outboxDDL); err != nil {
		return fmt.Errorf("create event_outbox: %w", err)
	}
	return nil
}
