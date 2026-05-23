package repo

import (
	"fmt"
	"strings"

	"gochen/db/query"
)

// applyAdvancedFilters 应用受控高级过滤条件。
func (r *Repo[T, ID]) applyAdvancedFilters(quer *queryBuilder, advanced query.AdvancedFilters) *queryBuilder {
	if len(advanced.Or) > 0 {
		expr, args := buildOrCondition(advanced.Or, quer.isAllowedField)
		if expr != "" {
			quer = quer.Where(expr, args...)
		}
	}
	if advanced.DateRange != nil {
		if advanced.DateRange.Start != "" {
			quer = quer.Where("created_at >= ?", advanced.DateRange.Start)
		}
		if advanced.DateRange.End != "" {
			quer = quer.Where("created_at <= ?", advanced.DateRange.End)
		}
	}
	return quer
}

// applySorting 应用Sorting。
func (r *Repo[T, ID]) applySorting(quer *queryBuilder, request *query.PageRequest) *queryBuilder {
	if request == nil {
		return quer
	}
	if len(request.Sorts) == 0 {
		return quer
	}
	for _, s := range request.Sorts {
		field := strings.TrimSpace(s.Field)
		direction := s.Direction
		if field == "" || !direction.IsValid() {
			continue
		}
		if !quer.isAllowedField(field) {
			continue
		}
		quer = quer.Order(field, direction == query.DESC)
	}
	return quer
}

// buildOrCondition 构造OrCondition。
func buildOrCondition(conditions []query.OrCondition, isAllowedField func(string) bool) (string, []any) {
	var exprs []string
	var args []any
	for _, condition := range conditions {
		if len(condition) == 0 {
			continue
		}
		var inner []string
		var innerArgs []any
		for key, value := range condition {
			if isAllowedField != nil && !isAllowedField(key) {
				continue
			}
			inner = append(inner, fmt.Sprintf("%s = ?", key))
			innerArgs = append(innerArgs, value)
		}
		if len(inner) > 0 {
			exprs = append(exprs, "("+strings.Join(inner, " AND ")+")")
			args = append(args, innerArgs...)
		}
	}
	return strings.Join(exprs, " OR "), args
}
