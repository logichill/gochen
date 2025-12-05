package sql

import (
	"context"
	"database/sql"
	"strings"

	core "gochen/data/db"
	"gochen/data/db/dialect"
)

type insertBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	table   string
	columns []string
	rows    [][]any
}

func (b *insertBuilder) Columns(cols ...string) IInsertBuilder {
	b.columns = cols
	return b
}

func (b *insertBuilder) Values(vals ...any) IInsertBuilder {
	if len(vals) == 0 {
		return b
	}
	b.rows = append(b.rows, vals)
	return b
}

func (b *insertBuilder) Build() (string, []any) {
	if len(b.columns) == 0 {
		panic("insertBuilder: Columns is required")
	}
	if len(b.rows) == 0 {
		panic("insertBuilder: at least one row is required")
	}

	var sb strings.Builder
	args := make([]any, 0, len(b.rows)*len(b.columns))

	sb.WriteString("INSERT INTO ")
	if !isSafeIdentifier(b.table) {
		panic("insertBuilder: unsafe table name " + b.table)
	}
	sb.WriteString(b.dialect.QuoteIdentifier(b.table))
	sb.WriteString(" (")
	quotedCols := make([]string, len(b.columns))
	for i, col := range b.columns {
		if !isSafeIdentifier(col) {
			panic("insertBuilder: unsafe column name " + col)
		}
		quotedCols[i] = b.dialect.QuoteIdentifier(col)
	}
	sb.WriteString(strings.Join(quotedCols, ", "))
	sb.WriteString(") VALUES ")

	rowPlaceholder := "(" + strings.TrimRight(strings.Repeat("?, ", len(b.columns)), ", ") + ")"

	for i, row := range b.rows {
		if len(row) != len(b.columns) {
			panic("insertBuilder: values length mismatch columns length")
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(rowPlaceholder)
		args = append(args, row...)
	}

	return sb.String(), args
}

func (b *insertBuilder) Exec(ctx context.Context) (sql.Result, error) {
	q, args := b.Build()
	return b.db.Exec(ctx, q, args...)
}
