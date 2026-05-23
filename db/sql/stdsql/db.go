package stdsql

import (
	"context"
	"database/sql"
	"gochen/contextx"
	core "gochen/db"
	"gochen/db/dialect"
	"gochen/errors"
	"time"
)

// DB 基于 database/sql 的最小实现，满足 core.IDatabase 抽象。
type DB struct {
	db     *sql.DB
	driver string
}

// New 创建一个 `core.IDatabase`，并使用库层默认上下文完成初始化。
func New(config core.DBConfig) (core.IDatabase, error) {
	// 使用库层兜底 Context，避免重复创建底层 *sql.DB 连接。
	return NewWithContext(contextx.Background(), config)
}

// NewWithContext 创建数据库适配器，并在初始化时执行一次可用性探测。
func NewWithContext(ctx context.Context, config core.DBConfig) (core.IDatabase, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	var (
		driver = config.Driver
		dsn    = config.Database
	)
	if driver == "" {
		driver = "sqlite"
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}

	// 连接池配置（可选）
	if config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(time.Duration(config.ConnMaxLifetime) * time.Second)
	}
	if config.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(time.Duration(config.ConnMaxIdleTime) * time.Second)
	}

	// 基础可用性检查
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &DB{db: db, driver: driver}, nil
}

// Query 执行查询语句，并按当前方言重绑定占位符。
func (d *DB) Query(ctx context.Context, query string, args ...any) (core.IRows, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if d == nil || d.db == nil {
		return nil, errors.NewCode(errors.InvalidInput, "db is nil")
	}
	dial := dialect.New(d.driver)
	q := dial.Rebind(query)
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

// QueryRow 执行只期望返回单行结果的查询。
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) core.IRow {
	if ctx == nil {
		return &Row{err: errors.NewCode(errors.InvalidInput, "ctx is nil")}
	}
	if d == nil || d.db == nil {
		return &Row{err: errors.NewCode(errors.InvalidInput, "db is nil")}
	}
	dial := dialect.New(d.driver)
	q := dial.Rebind(query)
	return &Row{row: d.db.QueryRowContext(ctx, q, args...)}
}

// Exec 执行非查询 SQL，并按当前方言重绑定占位符。
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if d == nil || d.db == nil {
		return nil, errors.NewCode(errors.InvalidInput, "db is nil")
	}
	dial := dialect.New(d.driver)
	q := dial.Rebind(query)
	return d.db.ExecContext(ctx, q, args...)
}

// Begin 以默认事务选项开启一个事务。
func (d *DB) Begin(ctx context.Context) (core.ITransaction, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if d == nil || d.db == nil {
		return nil, errors.NewCode(errors.InvalidInput, "db is nil")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{
		db:      d.db,
		tx:      tx,
		dialect: dialect.New(d.driver),
	}, nil
}

// BeginTx 按调用方提供的事务选项开启事务。
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (core.ITransaction, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if d == nil || d.db == nil {
		return nil, errors.NewCode(errors.InvalidInput, "db is nil")
	}
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{
		db:      d.db,
		tx:      tx,
		dialect: dialect.New(d.driver),
	}, nil
}

// Ping 对底层数据库连接做一次连通性探测。
func (d *DB) Ping(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if d == nil || d.db == nil {
		return errors.NewCode(errors.InvalidInput, "db is nil")
	}
	return d.db.PingContext(ctx)
}

// Close 关闭底层 `*sql.DB` 连接池。
func (d *DB) Close() error { return d.db.Close() }

// SQLDB 返回底层 *sql.DB，仅供 stdsql 适配层使用。
func (d *DB) SQLDB() *sql.DB { return d.db }

// DialectName 返回当前底层驱动名，用于上层选择 SQL 方言。
func (d *DB) DialectName() string {
	return d.driver
}

// ExecDDL 直接执行 DDL 语句；它不会经过方言占位符重绑定。
func (d *DB) ExecDDL(sqlStmt string) error {
	if d.db == nil {
		return errors.NewCode(errors.InvalidInput, "db is nil")
	}
	_, err := d.db.Exec(sqlStmt)
	return err
}
