// Package database 提供通用的数据库抽象接口
//
// 设计目标：
// 1. 隔离具体的 ORM/SQL 库（GORM、SQLX等）
// 2. 提供统一的数据库操作接口
// 3. 支持事务操作
// 4. 便于单元测试（Mock）
package db

import (
	"context"
	"database/sql"
)

// IDatabase 通用数据库接口
type IDatabase interface {
	// 查询操作
	Query(ctx context.Context, query string, args ...any) (IRows, error)
	QueryRow(ctx context.Context, query string, args ...any) IRow

	// 执行操作
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)

	// 事务操作
	Begin(ctx context.Context) (ITransaction, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (ITransaction, error)

	// 连接管理
	Ping(ctx context.Context) error
	Close() error

	// 获取原始连接（用于特殊场景）
	Raw() any
}

// IDialectNameProvider 可选接口：提供底层数据库方言名称
//
// 实现方应返回诸如 "mysql"、"sqlite"、"postgres" 等 driver/dialect 名，
// 供 shared 层推断方言能力（如 DELETE LIMIT、唯一键错误识别等）。
type IDialectNameProvider interface {
	// GetDialectName 返回底层数据库方言名称
	GetDialectName() string
}

// ITransaction 事务接口
type ITransaction interface {
	IDatabase

	// 事务控制
	Commit() error
	Rollback() error
}

// IRows 查询结果集接口
type IRows interface {
	// 遍历结果
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error

	// 获取列信息
	Columns() ([]string, error)
	ColumnTypes() ([]*sql.ColumnType, error)
}

// IRow 单行结果接口
type IRow interface {
	Scan(dest ...any) error
	Err() error
}

// IQueryBuilder 查询构建器接口（可选，用于复杂查询）
type IQueryBuilder interface {
	Select(columns ...string) IQueryBuilder
	From(table string) IQueryBuilder
	Where(condition string, args ...any) IQueryBuilder
	OrderBy(column string, desc bool) IQueryBuilder
	Limit(limit int) IQueryBuilder
	Offset(offset int) IQueryBuilder

	Build() (query string, args []any)
}

// DBConfig 数据库配置
type DBConfig struct {
	Driver   string // mysql, postgres, sqlite, etc.
	Host     string
	Port     int
	Database string
	Username string
	Password string

	// 连接池配置
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int // 秒
	ConnMaxIdleTime int // 秒

	// 其他选项
	Charset   string
	ParseTime bool
	Location  string
}

// NewDatabase 工厂方法（由具体实现提供）
type NewDatabaseFunc func(config DBConfig) (IDatabase, error)
