package sql

import (
	"context"
	"strings"

	core "gochen/storage/database"
	"gochen/storage/database/dialect"
)

type selectBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	cols    []string
	table   string
	where   []string
	args    []interface{}
	groupBy []string
	orderBy string
	limit   int
	offset  int
}

func (b *selectBuilder) From(table string) ISelectBuilder {
	b.table = table
	return b
}

func (b *selectBuilder) Where(cond string, args ...interface{}) ISelectBuilder {
	if cond != "" {
		b.where = append(b.where, cond)
		b.args = append(b.args, args...)
	}
	return b
}

func (b *selectBuilder) And(cond string, args ...interface{}) ISelectBuilder {
	return b.Where(cond, args...)
}

func (b *selectBuilder) Or(cond string, args ...interface{}) ISelectBuilder {
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

func (b *selectBuilder) Build() (string, []interface{}) {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(b.cols, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(b.table)

	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " AND "))
	}
	if len(b.groupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(b.groupBy, ", "))
	}
	if b.orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(b.orderBy)
	}
	if b.limit > 0 {
		sb.WriteString(" LIMIT ?")
		b.args = append(b.args, b.limit)
	}
	if b.offset > 0 {
		sb.WriteString(" OFFSET ?")
		b.args = append(b.args, b.offset)
	}
	return sb.String(), b.args
}

func (b *selectBuilder) Query(ctx context.Context) (core.IRows, error) {
	q, args := b.Build()
	return b.db.Query(ctx, q, args...)
}

func (b *selectBuilder) QueryRow(ctx context.Context) core.IRow {
	q, args := b.Build()
	return b.db.QueryRow(ctx, q, args...)
}
