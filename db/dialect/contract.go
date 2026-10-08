// Package dialect 定义数据库方言契约；SQL 渲染和标准方言实现位于 gochen-runtime/db/dialect。
package dialect

// Name 标准化的数据库方言名称。
type Name string

const (
	// NameMySQL 表示 MySQL 方言。
	NameMySQL Name = "mysql"
	// NameSQLite 表示 SQLite 方言。
	NameSQLite Name = "sqlite"
	// NamePostgres 表示 PostgreSQL 方言。
	NamePostgres Name = "postgres"
	// NameUnknown 表示无法识别的方言。
	NameUnknown Name = ""
)

// IDialect 表示当前数据库的方言能力。
//
// 目前只抽象项目实际用到的能力：
//   - DeleteLimit: 是否支持 DELETE ... LIMIT
//   - UniqueViolation: 唯一键/主键冲突错误识别。
type IDialect interface {
	Name() Name
	QuoteIdentifier(name string) string
	Rebind(query string) string
	PlaceholderStyle() PlaceholderStyle
	SelectLockClause(skipLocked bool) (string, error)
	LikeEscapeClause() string
	SchemaFeatures() SchemaFeatures
	CreateIndexTargets(table string, indexName string) (string, string)
	DropIndexSQL(table string, indexName string) string
	MigrationStateUpsertSQL(table string) (string, bool)
	IntrospectionKey() string
	SupportsDeleteLimit() bool
	SupportsSavepoints() bool
	IsUniqueViolation(err error) bool
}

// PlaceholderStyle 描述占位符扫描时需要保留的方言语义。
type PlaceholderStyle uint8

const (
	PlaceholderQuestion PlaceholderStyle = iota
	PlaceholderPostgres
)

// AutoIncrementStyle 描述 DDL 自增列的渲染策略。
type AutoIncrementStyle uint8

const (
	AutoIncrementUnsupported AutoIncrementStyle = iota
	AutoIncrementMySQL
	AutoIncrementSQLite
	AutoIncrementPostgresSerial
)

// SchemaFeatures 汇总 schema renderer 所需的方言策略。
type SchemaFeatures struct {
	AddNotNullColumnRequiresDefault bool
	AutoIncrement                   AutoIncrementStyle
}

// IDialectProvider 允许数据库适配器直接提供完整方言能力。
// 它优先于仅提供名称的 db.IDialectNameProvider。
type IDialectProvider interface {
	Dialect() IDialect
}
