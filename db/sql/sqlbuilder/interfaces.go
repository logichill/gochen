package sqlbuilder

import (
	"context"
	"database/sql"

	core "gochen/db"
	"gochen/db/dialect"
	"gochen/errors"
)

// ISql 提供统一的 SQL 构建与执行接口。
type ISql interface {
	Select(columns ...string) ISelectBuilder
	InsertInto(table string) IInsertBuilder
	Update(table string) IUpdateBuilder
	DeleteFrom(table string) IDeleteBuilder
	UpsertInto(table string) IUpsertBuilder

	// GetDB 返回底层 IDatabase（仅示例/特殊场景使用）。
	// 常规业务不建议依赖此方法，而在构造层显式注入 IDatabase。
	GetDB() core.IDatabase
}

// ISelectBuilder 构建 SELECT 语句。
type ISelectBuilder interface {
	From(table string) ISelectBuilder
	// FromUnsafe 直接设置 FROM 表达式（如子查询/复杂 join 表达式；调用方需确保安全）。
	FromUnsafe(expr string) ISelectBuilder
	Where(cond string, args ...any) ISelectBuilder
	And(cond string, args ...any) ISelectBuilder
	Or(cond string, args ...any) ISelectBuilder
	GroupBy(cols ...string) ISelectBuilder
	// OrderBy 以“列 + 方向”的结构化方式声明排序（会做标识符安全校验并按方言 quote）。
	OrderBy(orders ...Order) ISelectBuilder
	// OrderByUnsafe 直接追加原始 ORDER BY 表达式（调用方需确保安全）。
	OrderByUnsafe(expr string) ISelectBuilder
	Limit(n int) ISelectBuilder
	Offset(n int) ISelectBuilder
	ForUpdate() ISelectBuilder
	SkipLocked() ISelectBuilder
	Build() (query string, args []any, err error)
	Query(ctx context.Context) (core.IRows, error)
	QueryRow(ctx context.Context) core.IRow
}

// Order 描述 ORDER BY 的单个排序项。
type Order struct {
	Column string
	Desc   bool
}

// OrderAsc 创建升序排序项。
func OrderAsc(column string) Order { return Order{Column: column} }

// OrderDesc 创建降序排序项。
func OrderDesc(column string) Order { return Order{Column: column, Desc: true} }

// IInsertBuilder 构建 INSERT 语句。
type IInsertBuilder interface {
	Columns(cols ...string) IInsertBuilder
	Values(vals ...any) IInsertBuilder
	Build() (query string, args []any, err error)
	Exec(ctx context.Context) (sql.Result, error)
}

// IUpdateBuilder 构建 UPDATE 语句。
type IUpdateBuilder interface {
	Set(column string, val any) IUpdateBuilder
	SetMap(values map[string]any) IUpdateBuilder
	// SetIncrement 追加一个“列自增/自减” SET 子句（col = col + ?），会做标识符安全校验并按方言 quote。
	SetIncrement(column string, delta any) IUpdateBuilder
	// SetCaseWhenEq 追加一个 CASE WHEN 形式的 SET 子句（如 col = CASE WHEN id = ? THEN ? ... END）。
	SetCaseWhenEq(column string, whenColumn string, cases []CaseWhenEq) IUpdateBuilder
	// SetExprUnsafe 直接追加原始 SET 片段（调用方需确保安全）。
	SetExprUnsafe(expr string, args ...any) IUpdateBuilder
	Where(cond string, args ...any) IUpdateBuilder
	Build() (query string, args []any, err error)
	Exec(ctx context.Context) (sql.Result, error)
}

// CaseWhenEq 表示 `CASE WHEN x = ? THEN ?` 中的一组分支值。
type CaseWhenEq struct {
	When any
	Then any
}

// IDeleteBuilder 构建 DELETE 语句。
type IDeleteBuilder interface {
	Where(cond string, args ...any) IDeleteBuilder
	Limit(n int) IDeleteBuilder
	Build() (query string, args []any, err error)
	Exec(ctx context.Context) (sql.Result, error)
}

// IUpsertBuilder 构建 UPSERT 语句。
type IUpsertBuilder interface {
	Columns(cols ...string) IUpsertBuilder
	Values(vals ...any) IUpsertBuilder
	Key(cols ...string) IUpsertBuilder
	UpdateSet(col string, val any) IUpsertBuilder
	UpdateSetMap(values map[string]any) IUpsertBuilder
	Exec(ctx context.Context) (sql.Result, error)
}

type sqlImpl struct {
	db      core.IDatabase
	dialect dialect.Dialect
}

// New 基于数据库适配器创建一组 SQL builder。
func New(db core.IDatabase) (ISql, error) {
	if db == nil {
		return nil, errors.NewCode(errors.InvalidInput, "sqlbuilder.New: database cannot be nil")
	}
	return &sqlImpl{
		db:      db,
		dialect: dialect.FromDatabase(db),
	}, nil
}

// Select 创建一个 SELECT 构建器；未传列时默认使用 `*`。
func (s *sqlImpl) Select(columns ...string) ISelectBuilder {
	if len(columns) == 0 {
		columns = []string{"*"}
	}
	return &selectBuilder{
		db:      s.db,
		dialect: s.dialect,
		cols:    columns,
	}
}

// InsertInto 创建一个 INSERT 构建器。
func (s *sqlImpl) InsertInto(table string) IInsertBuilder {
	return &insertBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

// Update 创建一个 UPDATE 构建器。
func (s *sqlImpl) Update(table string) IUpdateBuilder {
	return &updateBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

// DeleteFrom 创建一个 DELETE 构建器。
func (s *sqlImpl) DeleteFrom(table string) IDeleteBuilder {
	return &deleteBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

// UpsertInto 创建一个 UPSERT 构建器。
func (s *sqlImpl) UpsertInto(table string) IUpsertBuilder {
	return &upsertBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

// GetDB 返回底层数据库适配器，供少量特殊场景复用。
func (s *sqlImpl) GetDB() core.IDatabase {
	return s.db
}
