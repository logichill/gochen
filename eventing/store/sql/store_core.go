package sql

import (
	"context"

	"gochen/data/db"
)

// SQLEventStore 基于通用 SQL 接口的事件存储
type SQLEventStore struct {
	db        db.IDatabase
	tableName string
}

func NewSQLEventStore(db db.IDatabase, tableName string) *SQLEventStore {
	if tableName == "" {
		tableName = "event_store"
	}
	return &SQLEventStore{db: db, tableName: tableName}
}

func (s *SQLEventStore) Init(ctx context.Context) error { return s.db.Ping(ctx) }
func (s *SQLEventStore) GetDB() db.IDatabase            { return s.db }
func (s *SQLEventStore) GetTableName() string           { return s.tableName }
