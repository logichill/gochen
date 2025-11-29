package sql

import (
	"context"

	"gochen/data/db"
)

// SQLEventStore 基于通用 SQL 接口的事件存储
type SQLEventStore struct {
	db        database.IDatabase
	tableName string
}

func NewSQLEventStore(db database.IDatabase, tableName string) *SQLEventStore {
	if tableName == "" {
		tableName = "event_store"
	}
	return &SQLEventStore{db: db, tableName: tableName}
}

func (s *SQLEventStore) Init(ctx context.Context) error { return s.db.Ping(ctx) }
func (s *SQLEventStore) GetDB() database.IDatabase      { return s.db }
func (s *SQLEventStore) GetTableName() string           { return s.tableName }
