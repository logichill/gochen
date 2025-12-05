package sql

import (
	"context"
	"strings"

	core "gochen/data/db"
	"gochen/data/db/dialect"
)

type selectBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	cols    []string
	table   string
	where   []string
	args    []any
	groupBy []string
	orderBy string
	limit   int
	offset  int
	locking string
}

func (b *selectBuilder) From(table string) ISelectBuilder {
	b.table = table
	return b
}

func (b *selectBuilder) Where(cond string, args ...any) ISelectBuilder {
	if cond != "" {
		b.where = append(b.where, cond)
		b.args = append(b.args, args...)
	}
	return b
}

func (b *selectBuilder) And(cond string, args ...any) ISelectBuilder {
	return b.Where(cond, args...)
}

func (b *selectBuilder) Or(cond string, args ...any) ISelectBuilder {
	if cond == "" {
		return b
	}
	if len(b.where) == 0 {
		return b.Where(cond, args...)
	}
	last := b.where[len(b.where)-1]
	b.where[len(b.where)-1] = "(" + last + " OR " + cond + ")"
	b.args = append(b.args, args...)
	return b
}

func (b *selectBuilder) GroupBy(cols ...string) ISelectBuilder {
	if len(cols) > 0 {
		b.groupBy = append(b.groupBy, cols...)
	}
	return b
}

func (b *selectBuilder) OrderBy(expr string) ISelectBuilder {
	if expr != "" {
		b.orderBy = expr
	}
	return b
}

func (b *selectBuilder) Limit(n int) ISelectBuilder {
	b.limit = n
	return b
}

func (b *selectBuilder) Offset(n int) ISelectBuilder {
	b.offset = n
	return b
}

func (b *selectBuilder) ForUpdate() ISelectBuilder {
	switch b.dialect.Name() {
	case dialect.NameMySQL, dialect.NamePostgres:
		b.locking = " FOR UPDATE"
	default:
		// 对于不支持 FOR UPDATE 的方言（如 SQLite），忽略该设置以保持兼容
	}
	return b
}

func (b *selectBuilder) SkipLocked() ISelectBuilder {
	switch b.dialect.Name() {
	case dialect.NameMySQL, dialect.NamePostgres:
		if b.locking == "" {
			b.locking = " FOR UPDATE"
		}
		b.locking += " SKIP LOCKED"
	default:
		// 方言不支持 SKIP LOCKED 时忽略
	}
	return b
}

func (b *selectBuilder) Build() (string, []any) {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(b.buildSelectColumns())
	sb.WriteString(" FROM ")
	sb.WriteString(b.buildTableName())

	// 使用局部 args 副本，避免在多次 Build 调用之间污染 builder 状态。
	args := make([]any, 0, len(b.args)+2)
	args = append(args, b.args...)

	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " AND "))
	}
	if len(b.groupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(b.buildGroupBy())
	}
	if b.orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(b.orderBy)
	}
	if b.limit > 0 {
		sb.WriteString(" LIMIT ?")
		args = append(args, b.limit)
	}
	if b.offset > 0 {
		sb.WriteString(" OFFSET ?")
		args = append(args, b.offset)
	}
	if b.locking != "" {
		sb.WriteString(b.locking)
	}
	return sb.String(), args
}

func (b *selectBuilder) Query(ctx context.Context) (core.IRows, error) {
	q, args := b.Build()
	return b.db.Query(ctx, q, args...)
}

func (b *selectBuilder) QueryRow(ctx context.Context) core.IRow {
	q, args := b.Build()
	return b.db.QueryRow(ctx, q, args...)
}

// buildTableName 构建 FROM 子句中的表名，必要时按方言加引号。
func (b *selectBuilder) buildTableName() string {
	if isSafeIdentifier(b.table) {
		return b.dialect.QuoteIdentifier(b.table)
	}
	// 非“纯标识符”场景（如子查询/表达式）保持原样，由调用方负责安全性。
	return b.table
}

// buildSelectColumns 构建 SELECT 列表，对“看起来像标识符”的列名按方言加引号。
// 特殊值 "*" 不加引号。
func (b *selectBuilder) buildSelectColumns() string {
	cols := make([]string, len(b.cols))
	for i, col := range b.cols {
		if col == "*" {
			cols[i] = col
			continue
		}
		if isSafeIdentifier(col) {
			cols[i] = b.dialect.QuoteIdentifier(col)
		} else {
			// 表达式类列名（如 COUNT(1)）保持原样。
			cols[i] = col
		}
	}
	return strings.Join(cols, ", ")
}

// buildGroupBy 构建 GROUP BY 列表，对安全标识符按方言加引号。
func (b *selectBuilder) buildGroupBy() string {
	group := make([]string, len(b.groupBy))
	for i, col := range b.groupBy {
		if isSafeIdentifier(col) {
			group[i] = b.dialect.QuoteIdentifier(col)
		} else {
			group[i] = col
		}
	}
	return strings.Join(group, ", ")
}
