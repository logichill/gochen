package stdsql

import (
	"context"
	"database/sql"
	core "gochen/db"
	"gochen/db/dialect"
	"gochen/errors"
)

// Tx 是 `database/sql` 事务在框架数据库抽象下的适配实现。
type Tx struct {
	db      *sql.DB
	tx      *sql.Tx
	dialect dialect.Dialect
}

// Query 在事务上下文内执行查询，并按当前方言重绑定占位符。
func (t *Tx) Query(ctx context.Context, query string, args ...any) (core.IRows, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if t == nil || t.tx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "tx is nil")
	}
	q := t.dialect.Rebind(query)
	rows, err := t.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

// QueryRow 在事务上下文内执行单行查询。
func (t *Tx) QueryRow(ctx context.Context, query string, args ...any) core.IRow {
	if ctx == nil {
		return &Row{err: errors.NewCode(errors.InvalidInput, "ctx is nil")}
	}
	if t == nil || t.tx == nil {
		return &Row{err: errors.NewCode(errors.InvalidInput, "tx is nil")}
	}
	q := t.dialect.Rebind(query)
	return &Row{row: t.tx.QueryRowContext(ctx, q, args...)}
}

// Exec 在事务上下文内执行非查询 SQL。
func (t *Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if t == nil || t.tx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "tx is nil")
	}
	q := t.dialect.Rebind(query)
	return t.tx.ExecContext(ctx, q, args...)
}

// Begin 明确拒绝嵌套事务，调用方如需细粒度回滚应使用 savepoint。
func (t *Tx) Begin(ctx context.Context) (core.ITransaction, error) {
	_ = ctx
	return nil, errors.NewCode(errors.Unsupported, "nested transactions are not supported")
}

// BeginTx 明确拒绝在现有事务内部再开启嵌套事务。
func (t *Tx) BeginTx(ctx context.Context, opts *sql.TxOptions) (core.ITransaction, error) {
	_ = ctx
	_ = opts
	return nil, errors.NewCode(errors.Unsupported, "nested transactions are not supported")
}

// Ping 使用底层 `*sql.DB` 做一次连通性探测。
func (t *Tx) Ping(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if t == nil || t.db == nil {
		return errors.NewCode(errors.InvalidInput, "db is nil")
	}
	return t.db.PingContext(ctx)
}

// Close 以“未提交则回滚”的语义结束事务句柄。
func (t *Tx) Close() error {
	if t == nil || t.tx == nil {
		return nil
	}
	// 安全语义：Close 等价于“未提交则回滚”，以支持常见的 `defer tx.Close()` 用法。
	// 若事务已 Commit/Rollback，则 Rollback 会返回 sql.ErrTxDone，视为幂等成功。
	if err := t.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return err
	}
	return nil
}

// SQLTx 返回底层 *sql.Tx，仅供 stdsql 适配层使用。
func (t *Tx) SQLTx() *sql.Tx { return t.tx }

// Commit 提交当前事务。
func (t *Tx) Commit() error { return t.tx.Commit() }

// CreateSavepoint 创建 savepoint。
func (t *Tx) CreateSavepoint(ctx context.Context, name string) error {
	_, err := t.Exec(ctx, "SAVEPOINT "+name)
	return err
}

// RollbackToSavepoint 回滚到指定 savepoint。
func (t *Tx) RollbackToSavepoint(ctx context.Context, name string) error {
	_, err := t.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name)
	return err
}

// ReleaseSavepoint 释放指定 savepoint。
func (t *Tx) ReleaseSavepoint(ctx context.Context, name string) error {
	_, err := t.Exec(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

// SupportsSavepoints 返回当前事务是否应暴露 savepoint 能力。
func (t *Tx) SupportsSavepoints() bool {
	if t == nil {
		return false
	}
	return t.dialect.SupportsSavepoints()
}

// Rollback 回滚当前事务。
func (t *Tx) Rollback() error { return t.tx.Rollback() }

func (t *Tx) DialectName() string {
	return string(t.dialect.Name())
}
