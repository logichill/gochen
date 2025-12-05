package sql

import (
	"context"
	"database/sql"
	"strings"

	core "gochen/data/db"
	"gochen/data/db/dialect"
)

type updateBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	table     string
	setCols   []string
	setArgs   []any
	exprSet   []string
	exprArgs  []any
	whereExpr []string
	whereArgs []any
}

func (b *updateBuilder) Set(col string, val any) IUpdateBuilder {
	if col == "" {
		return b
	}
	b.setCols = append(b.setCols, col)
	b.setArgs = append(b.setArgs, val)
	return b
}

func (b *updateBuilder) SetMap(values map[string]any) IUpdateBuilder {
	for k, v := range values {
		b.Set(k, v)
	}
	return b
}

func (b *updateBuilder) SetExpr(expr string, args ...any) IUpdateBuilder {
	if expr == "" {
		return b
	}
	b.exprSet = append(b.exprSet, expr)
	if len(args) > 0 {
		b.exprArgs = append(b.exprArgs, args...)
	}
	return b
}

func (b *updateBuilder) Where(cond string, args ...any) IUpdateBuilder {
	if cond != "" {
		b.whereExpr = append(b.whereExpr, cond)
		b.whereArgs = append(b.whereArgs, args...)
	}
	return b
}

func (b *updateBuilder) Build() (string, []any) {
	if len(b.setCols) == 0 && len(b.exprSet) == 0 {
		panic("updateBuilder: no columns or expressions to set")
	}

	var sb strings.Builder
	args := make([]any, 0, len(b.setArgs)+len(b.exprArgs)+len(b.whereArgs))

	sb.WriteString("UPDATE ")
	if !isSafeIdentifier(b.table) {
		panic("updateBuilder: unsafe table name " + b.table)
	}
	sb.WriteString(b.dialect.QuoteIdentifier(b.table))
	sb.WriteString(" SET ")

	first := true
	for i, col := range b.setCols {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		if !isSafeIdentifier(col) {
			panic("updateBuilder: unsafe column name " + col)
		}
		sb.WriteString(b.dialect.QuoteIdentifier(col))
		sb.WriteString(" = ?")
		args = append(args, b.setArgs[i])
	}
	for i, expr := range b.exprSet {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(expr)
		// exprArgs 在下面统一追加
		_ = i
	}

	args = append(args, b.exprArgs...)

	if len(b.whereExpr) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.whereExpr, " AND "))
		args = append(args, b.whereArgs...)
	}

	return sb.String(), args
}

func (b *updateBuilder) Exec(ctx context.Context) (sql.Result, error) {
	q, args := b.Build()
	return b.db.Exec(ctx, q, args...)
}
