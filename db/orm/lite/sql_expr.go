package lite

import (
	"strings"

	"gochen/db/dialect"
	"gochen/db/orm"
	"gochen/db/sql/safeident"
	"gochen/db/sql/sqlbuilder"
	"gochen/errors"
)

// buildTableExpr 执行对应操作。
func buildTableExpr(d dialect.Dialect, base string, joins []orm.Join) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.NewCode(errors.InvalidInput, "table name cannot be empty")
	}
	if !safeident.IsSafeIdentifier(base) {
		return "", errors.NewCode(errors.InvalidInput, "unsafe table name").WithContext("table", base)
	}
	if len(joins) == 0 {
		return base, nil
	}
	var sb strings.Builder
	// JOIN 场景走 FromUnsafe，需要在此处按方言 quote，避免出现“SELECT 被 quote 但 FROM 未 quote”的不一致。
	sb.WriteString(d.QuoteIdentifier(base))
	for _, j := range joins {
		switch j.Type {
		case orm.JoinInner, orm.JoinLeft, orm.JoinRight:
		default:
			return "", errors.NewCode(errors.InvalidInput, "invalid join type").WithContext("join_type", string(j.Type))
		}
		table := strings.TrimSpace(j.Table)
		if table == "" {
			return "", errors.NewCode(errors.InvalidInput, "join table cannot be empty")
		}
		if !safeident.IsSafeIdentifier(table) {
			return "", errors.NewCode(errors.InvalidInput, "unsafe join table").WithContext("table", table)
		}
		alias := strings.TrimSpace(j.Alias)
		if alias != "" {
			if strings.Contains(alias, ".") {
				return "", errors.NewCode(errors.InvalidInput, "join alias cannot contain dot").WithContext("alias", alias)
			}
			if !safeident.IsSafeIdentifier(alias) {
				return "", errors.NewCode(errors.InvalidInput, "unsafe join alias").WithContext("alias", alias)
			}
		}
		if len(j.On) == 0 {
			return "", errors.NewCode(errors.InvalidInput, "join on conditions cannot be empty").
				WithContext("table", table)
		}
		sb.WriteRune(' ')
		sb.WriteString(string(j.Type))
		sb.WriteString(" JOIN ")
		sb.WriteString(d.QuoteIdentifier(table))
		if alias != "" {
			sb.WriteRune(' ')
			sb.WriteString(d.QuoteIdentifier(alias))
		}
		sb.WriteString(" ON ")
		for i, on := range j.On {
			left := strings.TrimSpace(on.Left)
			right := strings.TrimSpace(on.Right)
			if left == "" || right == "" {
				return "", errors.NewCode(errors.InvalidInput, "join on identifiers cannot be empty").
					WithContext("table", table)
			}
			if !safeident.IsSafeIdentifier(left) {
				return "", errors.NewCode(errors.InvalidInput, "unsafe join on left identifier").WithContext("identifier", left)
			}
			if !safeident.IsSafeIdentifier(right) {
				return "", errors.NewCode(errors.InvalidInput, "unsafe join on right identifier").WithContext("identifier", right)
			}
			if i > 0 {
				sb.WriteString(" AND ")
			}
			sb.WriteString(d.QuoteIdentifier(left))
			sb.WriteString(" = ")
			sb.WriteString(d.QuoteIdentifier(right))
		}
	}
	return sb.String(), nil
}

// buildOrderBy 构造排序。
func buildOrderBy(orders []orm.OrderBy) ([]sqlbuilder.Order, error) {
	if len(orders) == 0 {
		return nil, nil
	}
	result := make([]sqlbuilder.Order, 0, len(orders))
	for _, o := range orders {
		if o.Column == "" {
			continue
		}
		if !safeident.IsSafeIdentifier(o.Column) {
			return nil, errors.NewCode(errors.InvalidInput, "invalid order by column").WithContext("column", o.Column)
		}
		result = append(result, sqlbuilder.Order{Column: o.Column, Desc: o.Desc})
	}
	return result, nil
}
