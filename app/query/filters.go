package query

import (
	"strings"

	"gochen/errors"
)

// FilterOp 表示过滤操作符。
type FilterOp string

const (
	// FilterOpEq 表示等于（=）。
	FilterOpEq FilterOp = "eq"
	// FilterOpNe 表示不等于（!=）。
	FilterOpNe FilterOp = "ne"
	// FilterOpLike 表示模糊匹配（具体语义由仓储实现决定，常见为 SQL LIKE）。
	FilterOpLike FilterOp = "like"
	// FilterOpGt 表示大于（>）。
	FilterOpGt FilterOp = "gt"
	// FilterOpGte 表示大于等于（>=）。
	FilterOpGte FilterOp = "gte"
	// FilterOpLt 表示小于（<）。
	FilterOpLt FilterOp = "lt"
	// FilterOpLte 表示小于等于（<=）。
	FilterOpLte FilterOp = "lte"
	// FilterOpIn 表示 IN 列表匹配（IN (...)）。
	FilterOpIn FilterOp = "in"
	// FilterOpNotIn 表示 NOT IN 列表匹配（NOT IN (...)）。
	FilterOpNotIn FilterOp = "not_in"
	// FilterOpIsNull 表示 IS NULL。
	FilterOpIsNull FilterOp = "is_null"
	// FilterOpNotNull 表示 IS NOT NULL。
	FilterOpNotNull FilterOp = "not_null"
)

// IsValid 判断操作符是否有效。
func (op FilterOp) IsValid() bool {
	switch op {
	case FilterOpEq, FilterOpNe, FilterOpLike,
		FilterOpGt, FilterOpGte, FilterOpLt, FilterOpLte,
		FilterOpIn, FilterOpNotIn,
		FilterOpIsNull, FilterOpNotNull:
		return true
	default:
		return false
	}
}

// ParseFilterOp 解析过滤条件Op。
func ParseFilterOp(s string) (FilterOp, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	op := FilterOp(s)
	return op, op.IsValid()
}

// Filter 表示适配层字符串过滤条件。
//
// 注意：
//   - 字段白名单/注入安全由仓储实现负责（例如 db/orm/repo 的 isAllowedField 校验）；
//   - 该类型只用于 HTTP/CLI 等动态协议输入；
//   - 进入 application/repository 前，应通过 QuerySchema 解码为 QueryFilters。
type Filter struct {
	Field  string   `json:"field"`
	Op     FilterOp `json:"op"`
	Value  string   `json:"value"`            // 二元操作符：单值（允许为空字符串）；一元操作符忽略该字段
	Values []string `json:"values,omitempty"` // in/not_in：多值
}

// DecodeAdapterFilters 将适配层字符串过滤条件提升为"默认 string 语义"的查询表达式集合。
//
// 无 QuerySchema 时无法做类型感知的范围比较，因此拒绝 gt/gte/lt/lte 操作符；
// 需要范围查询请配置 QuerySchema。
func DecodeAdapterFilters(filters []Filter) (QueryFilters, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	var out QueryFilters
	for _, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		if field == "" {
			continue
		}
		// 白名单校验：非法操作符（含注入片段）直接拒绝，不流入下游仓储。
		if !filter.Op.IsValid() {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter operator").
				WithContext("operator", string(filter.Op)).
				WithContext("field", field)
		}
		expr := QueryExpr{Op: filter.Op}
		switch filter.Op {
		case FilterOpGt, FilterOpGte, FilterOpLt, FilterOpLte:
			return nil, errors.NewCode(errors.InvalidInput, "range operator requires schema for type-aware decoding").
				WithContext("operator", string(filter.Op)).
				WithContext("field", field)
		case FilterOpIsNull, FilterOpNotNull:
			// 一元操作符不携带值，清空 value 避免下游误用。
			expr.Value = QueryValue{Type: FieldTypeString}
		case FilterOpIn, FilterOpNotIn:
			expr.Values = make([]QueryValue, 0, len(filter.Values))
			for _, value := range filter.Values {
				expr.Values = append(expr.Values, QueryValue{
					Type:       FieldTypeString,
					Normalized: value,
					String:     value,
				})
			}
		default:
			expr.Value = QueryValue{
				Type:       FieldTypeString,
				Normalized: filter.Value,
				String:     filter.Value,
			}
		}
		out = out.Append(field, expr)
	}
	if out.IsZero() {
		return nil, nil
	}
	return out, nil
}
