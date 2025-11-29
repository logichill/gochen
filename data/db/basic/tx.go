package basic

import (
	"context"
	"database/sql"

	core "gochen/data/db"
)

// Tx 事务实现，委托给 *sql.Tx，同时实现 core.IDatabase 以便透传给需要 DB 的接口
type Tx struct {
	db *sql.DB
	tx *sql.Tx
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (core.IRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) core.IRow {
	return &Row{row: t.tx.QueryRowContext(ctx, query, args...)}
}

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

// 嵌套事务：最小实现直接复用当前 tx（或返回不支持）。这里选择返回当前 tx。
func (t *Tx) Begin(ctx context.Context) (core.ITransaction, error) { return t, nil }
func (t *Tx) BeginTx(ctx context.Context, opts *sql.TxOptions) (core.ITransaction, error) {
	return t, nil
}

func (t *Tx) Ping(ctx context.Context) error { return t.db.PingContext(ctx) }
func (t *Tx) Close() error                   { return nil }
func (t *Tx) Raw() any                       { return t.tx }

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }
