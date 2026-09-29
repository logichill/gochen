// Package db 提供通用的数据访问端口与会话语义。
//
// 与 data 的分工：data 承载无 IO 的值语义与确定性契约原语；db 只承载端口与会话——
// 连接/事务（IDatabase）、方言识别（dialect）、ORM 适配契约（orm）。物理实现位于 runtime。
//
// 设计目标：
// 1. 隔离具体的 ORM/SQL 库（GORM、SQLX等）
// 2. 提供统一的数据库操作接口。
// 3. 支持事务操作。
// 4. 便于单元测试（Mock）
package db

import (
	"context"
	"database/sql"
	"fmt"
)

// IDatabase 通用数据库接口。
type IDatabase interface {
	// 查询操作。ctx 为 nil 时实现必须返回 InvalidInput；QueryRow 始终返回非 nil 行对象，
	// 延迟错误由其 Scan/Err 暴露。
	Query(ctx context.Context, query string, args ...any) (IRows, error)
	QueryRow(ctx context.Context, query string, args ...any) IRow

	// 执行操作
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)

	// 事务操作。嵌套事务可按实现契约返回 Unsupported。
	Begin(ctx context.Context) (ITransaction, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (ITransaction, error)

	// 连接管理
	Ping(ctx context.Context) error
	// Close 释放当前实现拥有的资源；借用事务视图的 Close 必须保持 no-op。
	Close() error
}

// IDialectNameProvider 抽象Dialect名称提供者能力接口。
type IDialectNameProvider interface {
	// DialectName 返回底层数据库方言名称
	DialectName() string
}

// IBindParameterLimitProvider 暴露单条语句可携带的占位参数上限。
//
// 批量操作（多值 INSERT、`id IN (...)`）据此分片。上限是**驱动**的属性，
// 因此由数据库实现回答，而不是让每个 ORM / 仓储各自持有一份配置——
// 一个进程里几十个仓储重复同一个事实，漏配一处就是一个潜伏到运行期的错误。
//
// 可选能力：未实现即使用 dialect.DefaultMaxBindParameters。
type IBindParameterLimitProvider interface {
	MaxBindParameters() int
}

// ITransaction 事务接口。
type ITransaction interface {
	IDatabase

	// 事务控制
	Commit() error
	Rollback() error
}

// ISavepointTransaction 抽象SavepointTransaction能力接口。
type ISavepointTransaction interface {
	ITransaction
	CreateSavepoint(ctx context.Context, name string) error
	RollbackToSavepoint(ctx context.Context, name string) error
	ReleaseSavepoint(ctx context.Context, name string) error
}

// ISavepointCapabilityProvider 抽象SavepointCapability提供者能力接口。
type ISavepointCapabilityProvider interface {
	SupportsSavepoints() bool
}

// IRows 查询结果集接口。
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

// IRow 单行结果接口。
type IRow interface {
	Scan(dest ...any) error
	Err() error
}

// DBConfig 数据库配置。
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

	// 扩展选项（用于特定数据库的额外配置）
	// 例如 PostgreSQL SSL: Options["sslmode"] = "require"
	Options map[string]any

	// Naming 配置切面列名约定（可选）
	Naming *NamingConvention

	// MaxBindParameters 覆盖单条语句可携带的占位参数上限。
	//
	// **正常不要设置。** 正确值由驱动决定，dialect.DefaultMaxBindParameters
	// 对全部受支持方言都成立；手填只会填错——填大了是运行期报错，
	// 填小了是白白把一条语句拆成十条。
	//
	// 它存在只为一种情况：接到上限更低的环境（典型是 CGO 链接了系统自带的
	// SQLite 3.32 之前版本，那时上限是 999）。0 表示用默认值。
	MaxBindParameters int
}

// String 返回脱敏后的配置字符串，避免在日志中泄露密码等敏感信息。
func (c DBConfig) String() string {
	return fmt.Sprintf("DBConfig{Driver=%s, Host=%s, Port=%d, Database=%s, Username=%s}",
		c.Driver, c.Host, c.Port, c.Database, c.Username)
}

// NewDatabase 工厂方法（由具体实现提供）
type NewDatabaseFunc func(config DBConfig) (IDatabase, error)
