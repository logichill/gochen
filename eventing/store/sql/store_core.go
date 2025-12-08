package sql

import (
	"context"

	"gochen/data/db"
	estore "gochen/eventing/store"
)

// SQLEventStore 基于通用 SQL 接口的事件存储
type SQLEventStore struct {
	db        db.IDatabase
	tableName string
}

// SQLEventStoreOptions SQL 事件存储构造选项。
//
// 说明：
//   - 当前仅包含表名，后续可扩展分区策略、索引前缀等。
type SQLEventStoreOptions struct {
	TableName string
}

func NewSQLEventStore(db db.IDatabase, tableName string) *SQLEventStore {
	if db == nil {
		panic("NewSQLEventStore: db cannot be nil")
	}
	if tableName == "" {
		tableName = "event_store"
	}
	return &SQLEventStore{db: db, tableName: tableName}
}

// NewSQLEventAggregateStore 创建实现聚合级 IEventStore[int64] 的 SQL 事件存储。
func NewSQLEventAggregateStore(db db.IDatabase, opts SQLEventStoreOptions) estore.IEventStore[int64] {
	return NewSQLEventStore(db, opts.TableName)
}

// NewSQLEventStreamStore 创建实现 IEventStreamStore[int64] 的 SQL 事件存储。
func NewSQLEventStreamStore(db db.IDatabase, opts SQLEventStoreOptions) estore.IEventStreamStore[int64] {
	return NewSQLEventStore(db, opts.TableName)
}

func (s *SQLEventStore) Init(ctx context.Context) error { return s.db.Ping(ctx) }
func (s *SQLEventStore) GetDB() db.IDatabase            { return s.db }
func (s *SQLEventStore) GetTableName() string           { return s.tableName }
