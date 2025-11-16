package sql

import (
	"context"
	"database/sql"

	core "gochen/storage/database"
	"gochen/storage/database/dialect"
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
	Where(cond string, args ...interface{}) ISelectBuilder
	And(cond string, args ...interface{}) ISelectBuilder
	Or(cond string, args ...interface{}) ISelectBuilder
	GroupBy(cols ...string) ISelectBuilder
	OrderBy(expr string) ISelectBuilder
	Limit(n int) ISelectBuilder
	Offset(n int) ISelectBuilder
	Build() (query string, args []interface{})
	Query(ctx context.Context) (core.IRows, error)
	QueryRow(ctx context.Context) core.IRow
}

// IInsertBuilder 构建 INSERT 语句。
type IInsertBuilder interface {
	Columns(cols ...string) IInsertBuilder
	Values(vals ...interface{}) IInsertBuilder
	Build() (query string, args []interface{})
	Exec(ctx context.Context) (sql.Result, error)
}

// IUpdateBuilder 构建 UPDATE 语句。
type IUpdateBuilder interface {
	Set(column string, val interface{}) IUpdateBuilder
	SetMap(values map[string]interface{}) IUpdateBuilder
	// SetExpr 直接追加原始 SET 片段（例如 "retry_count = retry_count + 1" 或包含 CASE 表达式），
	// 由调用方保证表达式合法性与参数顺序安全。
	SetExpr(expr string, args ...interface{}) IUpdateBuilder
	Where(cond string, args ...interface{}) IUpdateBuilder
	Build() (query string, args []interface{})
	Exec(ctx context.Context) (sql.Result, error)
}

// IDeleteBuilder 构建 DELETE 语句。
type IDeleteBuilder interface {
	Where(cond string, args ...interface{}) IDeleteBuilder
	Limit(n int) IDeleteBuilder
	Build() (query string, args []interface{})
	Exec(ctx context.Context) (sql.Result, error)
}

// IUpsertBuilder 构建 UPSERT 语句。
type IUpsertBuilder interface {
	Columns(cols ...string) IUpsertBuilder
	Values(vals ...interface{}) IUpsertBuilder
	Key(cols ...string) IUpsertBuilder
	UpdateSet(col string, val interface{}) IUpsertBuilder
	UpdateSetMap(values map[string]interface{}) IUpsertBuilder
	Exec(ctx context.Context) (sql.Result, error)
}

type sqlImpl struct {
	db      core.IDatabase
	dialect dialect.Dialect
}

// New 创建 ISql 实例。
func New(db core.IDatabase) ISql {
	return &sqlImpl{
		db:      db,
		dialect: dialect.FromDatabase(db),
	}
}

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

func (s *sqlImpl) InsertInto(table string) IInsertBuilder {
	return &insertBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

func (s *sqlImpl) Update(table string) IUpdateBuilder {
	return &updateBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

func (s *sqlImpl) DeleteFrom(table string) IDeleteBuilder {
	return &deleteBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

func (s *sqlImpl) UpsertInto(table string) IUpsertBuilder {
	return &upsertBuilder{
		db:      s.db,
		dialect: s.dialect,
		table:   table,
	}
}

func (s *sqlImpl) GetDB() core.IDatabase {
	return s.db
}
