package lite

import (
	"context"
	"database/sql"
	"gochen/contextx"
	"gochen/db"
	"gochen/db/orm"
	dbsql "gochen/db/sql/sqlbuilder"
	"gochen/errors"
	"reflect"
	"sync"
)

// Orm 定义Orm。
type Orm struct {
	db   db.IDatabase
	sql  dbsql.ISql
	caps orm.Capabilities

	mu        sync.RWMutex
	structMap map[reflect.Type]*structMeta
}

// New 创建orm.IOrm。
func New(db db.IDatabase) (orm.IOrm, error) {
	sql, err := dbsql.New(db)
	if err != nil {
		return nil, err
	}
	return &Orm{
		db:  db,
		sql: sql,
		caps: orm.NewCapabilities(
			orm.CapabilityBasicCRUD,
			orm.CapabilityQuery,
			orm.CapabilityTransaction,
		),
		structMap: make(map[reflect.Type]*structMeta),
	}, nil
}

func (o *Orm) Capabilities() orm.Capabilities { return o.caps }

func (o *Orm) WithContext(ctx context.Context) orm.IOrm { // nolint: revive
	_ = ctx
	return o
}

func (o *Orm) Model(meta *orm.ModelMeta) (orm.IModel, error) {
	if meta == nil {
		return nil, errors.NewCode(errors.InvalidInput, "orm model meta cannot be nil")
	}

	table := meta.Table
	if table == "" {
		// 如果模型实现了 TableName()，优先使用
		if model := meta.NewModel(); model != nil {
			if tn, ok := tryGetTableName(model); ok {
				table = tn
			}
		}
	}
	if table == "" {
		return nil, errors.NewCode(errors.InvalidInput, "orm table name is empty")
	}

	return &model{
		orm:   o,
		meta:  meta,
		table: table,
	}, nil
}

// Begin 开启事务会话。
func (o *Orm) Begin(ctx context.Context) (orm.IOrmSession, error) {
	tx, err := o.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	txOrm, err := New(tx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	typed, ok := txOrm.(*Orm)
	if !ok {
		_ = tx.Rollback()
		return nil, errors.NewCode(errors.Internal, "unexpected orm type")
	}
	return &session{
		Orm:         typed,
		tx:          tx,
		afterCommit: contextx.NewAfterCommitDispatcher(),
	}, nil
}

// BeginTx 开启带选项的事务会话。
func (o *Orm) BeginTx(ctx context.Context, opts *sql.TxOptions) (orm.IOrmSession, error) {
	tx, err := o.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	txOrm, err := New(tx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	typed, ok := txOrm.(*Orm)
	if !ok {
		_ = tx.Rollback()
		return nil, errors.NewCode(errors.Internal, "unexpected orm type")
	}
	return &session{
		Orm:         typed,
		tx:          tx,
		afterCommit: contextx.NewAfterCommitDispatcher(),
	}, nil
}

func (o *Orm) Database() db.IDatabase { return o.db }
