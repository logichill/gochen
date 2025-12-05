package sql

import (
	"context"
	"database/sql"
	"strings"

	core "gochen/data/db"
	"gochen/data/db/dialect"
)

type deleteBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	table string
	where []string
	args  []any
	limit int
}

func (b *deleteBuilder) Where(cond string, args ...any) IDeleteBuilder {
	if cond != "" {
		b.where = append(b.where, cond)
		b.args = append(b.args, args...)
	}
	return b
}

func (b *deleteBuilder) Limit(n int) IDeleteBuilder {
	b.limit = n
	return b
}

func (b *deleteBuilder) Build() (string, []any) {
	var sb strings.Builder
	args := make([]any, len(b.args))
	copy(args, b.args)

	sb.WriteString("DELETE FROM ")
	if !isSafeIdentifier(b.table) {
		panic("deleteBuilder: unsafe table name " + b.table)
	}
	sb.WriteString(b.dialect.QuoteIdentifier(b.table))

	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " AND "))
	}

	if b.limit > 0 && b.dialect.SupportsDeleteLimit() {
		sb.WriteString(" LIMIT ?")
		args = append(args, b.limit)
	}

	return sb.String(), args
}

func (b *deleteBuilder) Exec(ctx context.Context) (sql.Result, error) {
	q, args := b.Build()
	return b.db.Exec(ctx, q, args...)
}
