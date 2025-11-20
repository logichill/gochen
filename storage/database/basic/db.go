package basic

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	core "gochen/storage/database"
	"gochen/storage/database/dialect"
)

// DB 基于 database/sql 的最小实现，满足 core.IDatabase 抽象
type DB struct {
	db     *sql.DB
	driver string
}

// New 根据 core.DBConfig 创建基础数据库实例
// 仅做最小封装：
// - 调用方必须确保所配置的 Driver 已通过空导入注册（例如在上层显式 `_ "modernc.org/sqlite"`）
// - sqlite 场景下建议在应用或测试层自行 import 驱动，basic 层只负责最小抽象
func New(config core.DBConfig) (core.IDatabase, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &DB{db: db, driver: driver}, nil
}

func (d *DB) Query(ctx context.Context, query string, args ...interface{}) (core.IRows, error) {
	dial := dialect.New(d.driver)
	q := dial.Rebind(query)
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

func (d *DB) QueryRow(ctx context.Context, query string, args ...interface{}) core.IRow {
	dial := dialect.New(d.driver)
	q := dial.Rebind(query)
	return &Row{row: d.db.QueryRowContext(ctx, q, args...)}
}

func (d *DB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	dial := dialect.New(d.driver)
	q := dial.Rebind(query)
	return d.db.ExecContext(ctx, q, args...)
}

func (d *DB) Begin(ctx context.Context) (core.ITransaction, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{db: d.db, tx: tx}, nil
}

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (core.ITransaction, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{db: d.db, tx: tx}, nil
}

func (d *DB) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }
func (d *DB) Close() error                   { return d.db.Close() }
func (d *DB) Raw() interface{}               { return d.db }

// GetDialectName 实现 core.DialectNameProvider 接口，返回底层 driver 名
func (d *DB) GetDialectName() string {
	return d.driver
}

// MustExecDDL 辅助：执行 DDL（用于测试环境）
func (d *DB) MustExecDDL(sqlStmt string) error {
	if d.db == nil {
		return fmt.Errorf("db is nil")
	}
	_, err := d.db.Exec(sqlStmt)
	return err
}
