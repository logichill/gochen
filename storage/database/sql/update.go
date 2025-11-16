package sql

import (
	"context"
	"database/sql"
	"strings"

	core "gochen/storage/database"
	"gochen/storage/database/dialect"
)

type updateBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	table     string
	setCols   []string
	setArgs   []interface{}
	exprSet   []string
	exprArgs  []interface{}
	whereExpr []string
	whereArgs []interface{}
}

func (b *updateBuilder) Set(col string, val interface{}) IUpdateBuilder {
	if col == "" {
		return b
	}
	b.setCols = append(b.setCols, col)
	b.setArgs = append(b.setArgs, val)
	return b
}

func (b *updateBuilder) SetMap(values map[string]interface{}) IUpdateBuilder {
	for k, v := range values {
		b.Set(k, v)
	}
	return b
}

func (b *updateBuilder) SetExpr(expr string, args ...interface{}) IUpdateBuilder {
	if expr == "" {
		return b
	}
	b.exprSet = append(b.exprSet, expr)
	if len(args) > 0 {
		b.exprArgs = append(b.exprArgs, args...)
	}
	return b
}

func (b *updateBuilder) Where(cond string, args ...interface{}) IUpdateBuilder {
	if cond != "" {
		b.whereExpr = append(b.whereExpr, cond)
		b.whereArgs = append(b.whereArgs, args...)
	}
	return b
}

func (b *updateBuilder) Build() (string, []interface{}) {
	if len(b.setCols) == 0 && len(b.exprSet) == 0 {
		panic("updateBuilder: no columns or expressions to set")
	}

	var sb strings.Builder
	args := make([]interface{}, 0, len(b.setArgs)+len(b.exprArgs)+len(b.whereArgs))

	sb.WriteString("UPDATE ")
	sb.WriteString(b.table)
	sb.WriteString(" SET ")

	first := true
	for i, col := range b.setCols {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		sb.WriteString(col)
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
