package basic

import (
	"context"
	"database/sql"
	"fmt"

	core "gochen/data/db"
	"gochen/data/db/dialect"
)

// Tx 事务实现，委托给 *sql.Tx，同时实现 core.IDatabase 以便透传给需要 DB 的接口
type Tx struct {
	db      *sql.DB
	tx      *sql.Tx
	dialect dialect.Dialect
}

func (t *Tx) Query(ctx context.Context, query string, args ...any) (core.IRows, error) {
	q := t.dialect.Rebind(query)
	rows, err := t.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) core.IRow {
	q := t.dialect.Rebind(query)
	return &Row{row: t.tx.QueryRowContext(ctx, q, args...)}
}

func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q := t.dialect.Rebind(query)
	return t.tx.ExecContext(ctx, q, args...)
}

// 嵌套事务：当前 basic.Tx 明确不支持嵌套事务，调用方应在上层协调事务边界。
func (t *Tx) Begin(ctx context.Context) (core.ITransaction, error) {
	return nil, fmt.Errorf("basic.Tx: nested transactions are not supported")
}

func (t *Tx) BeginTx(ctx context.Context, opts *sql.TxOptions) (core.ITransaction, error) {
	return nil, fmt.Errorf("basic.Tx: nested transactions are not supported")
}

func (t *Tx) Ping(ctx context.Context) error { return t.db.PingContext(ctx) }
func (t *Tx) Close() error                   { return nil }
func (t *Tx) Raw() any                       { return t.tx }

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

// GetDialectName 实现 core.IDialectNameProvider，便于在事务上下文中复用方言能力。
func (t *Tx) GetDialectName() string {
	return string(t.dialect.Name())
}
