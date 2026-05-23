package repo

import (
	"sort"
	"strings"

	"gochen/db/query"
)

func (quer *queryBuilder) withFilters(filters []query.Filter) *queryBuilder {
	for _, f := range filters {
		field := strings.TrimSpace(f.Field)
		if field == "" || !quer.isAllowedField(field) {
			continue
		}

		switch f.Op {
		case query.FilterOpEq:
			quer = quer.Where(field+" = ?", f.Value)
		case query.FilterOpNe:
			quer = quer.Where(field+" != ?", f.Value)
		case query.FilterOpLike:
			quer = quer.Where(field+" LIKE ?", "%"+f.Value+"%")
		case query.FilterOpGt:
			quer = quer.Where(field+" > ?", f.Value)
		case query.FilterOpGte:
			quer = quer.Where(field+" >= ?", f.Value)
		case query.FilterOpLt:
			quer = quer.Where(field+" < ?", f.Value)
		case query.FilterOpLte:
			quer = quer.Where(field+" <= ?", f.Value)
		case query.FilterOpIn:
			if len(f.Values) > 0 {
				quer = quer.Where(field+" IN ?", f.Values)
			}
		case query.FilterOpNotIn:
			if len(f.Values) > 0 {
				quer = quer.Where(field+" NOT IN ?", f.Values)
			}
		case query.FilterOpIsNull:
			quer = quer.Where(field + " IS NULL")
		case query.FilterOpNotNull:
			quer = quer.Where(field + " IS NOT NULL")
		default:
			continue
		}
	}
	return quer
}

func (quer *queryBuilder) withQueryFilters(filters query.QueryFilters) *queryBuilder {
	if filters.IsZero() {
		return quer
	}
	fields := make([]string, 0, len(filters))
	for field, exprs := range filters {
		if strings.TrimSpace(field) == "" || len(exprs) == 0 {
			continue
		}
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" || !quer.isAllowedField(field) {
			continue
		}
		for _, expr := range filters[field] {
			switch expr.Op {
			case query.FilterOpEq:
				quer = quer.Where(field+" = ?", expr.Value.Any())
			case query.FilterOpNe:
				quer = quer.Where(field+" != ?", expr.Value.Any())
			case query.FilterOpLike:
				quer = quer.Where(field+" LIKE ?", "%"+expr.Value.Normalized+"%")
			case query.FilterOpGt:
				quer = quer.Where(field+" > ?", expr.Value.Any())
			case query.FilterOpGte:
				quer = quer.Where(field+" >= ?", expr.Value.Any())
			case query.FilterOpLt:
				quer = quer.Where(field+" < ?", expr.Value.Any())
			case query.FilterOpLte:
				quer = quer.Where(field+" <= ?", expr.Value.Any())
			case query.FilterOpIn:
				if len(expr.Values) > 0 {
					quer = quer.Where(field+" IN ?", queryExprArgs(expr.Values))
				}
			case query.FilterOpNotIn:
				if len(expr.Values) > 0 {
					quer = quer.Where(field+" NOT IN ?", queryExprArgs(expr.Values))
				}
			case query.FilterOpIsNull:
				quer = quer.Where(field + " IS NULL")
			case query.FilterOpNotNull:
				quer = quer.Where(field + " IS NOT NULL")
			default:
				continue
			}
		}
	}
	return quer
}

func queryExprArgs(values []query.QueryValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value.Any())
	}
	return out
}
