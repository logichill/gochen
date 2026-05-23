package query

import "strings"

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

// FilterBuilder 定义过滤条件构建器。
type FilterBuilder struct {
	filters []Filter
}

// NewFilterBuilder 创建新的过滤条件构建器。
func NewFilterBuilder() *FilterBuilder {
	return &FilterBuilder{}
}

// addValue 添加值。
func (b *FilterBuilder) addValue(field string, op FilterOp, value string) *FilterBuilder {
	field = strings.TrimSpace(field)
	if field == "" {
		return b
	}
	b.filters = append(b.filters, Filter{
		Field: field,
		Op:    op,
		Value: value,
	})
	return b
}

// addValues 添加值集合。
func (b *FilterBuilder) addValues(field string, op FilterOp, values []string) *FilterBuilder {
	field = strings.TrimSpace(field)
	if field == "" {
		return b
	}
	if len(values) == 0 {
		return b
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return b
	}
	b.filters = append(b.filters, Filter{
		Field:  field,
		Op:     op,
		Values: out,
	})
	return b
}

func (b *FilterBuilder) Eq(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpEq, value)
}

func (b *FilterBuilder) Ne(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpNe, value)
}

func (b *FilterBuilder) Like(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpLike, value)
}

func (b *FilterBuilder) Gt(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpGt, value)
}

func (b *FilterBuilder) Gte(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpGte, value)
}

func (b *FilterBuilder) Lt(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpLt, value)
}

func (b *FilterBuilder) Lte(field string, value string) *FilterBuilder {
	return b.addValue(field, FilterOpLte, value)
}

func (b *FilterBuilder) In(field string, values ...string) *FilterBuilder {
	if len(values) == 0 {
		return b
	}
	return b.addValues(field, FilterOpIn, values)
}

func (b *FilterBuilder) NotIn(field string, values ...string) *FilterBuilder {
	if len(values) == 0 {
		return b
	}
	return b.addValues(field, FilterOpNotIn, values)
}

// IsNull 判断Null。
func (b *FilterBuilder) IsNull(field string) *FilterBuilder {
	field = strings.TrimSpace(field)
	if field == "" {
		return b
	}
	b.filters = append(b.filters, Filter{
		Field: field,
		Op:    FilterOpIsNull,
	})
	return b
}

func (b *FilterBuilder) NotNull(field string) *FilterBuilder {
	field = strings.TrimSpace(field)
	if field == "" {
		return b
	}
	b.filters = append(b.filters, Filter{
		Field: field,
		Op:    FilterOpNotNull,
	})
	return b
}

func (b *FilterBuilder) Build() []Filter {
	if len(b.filters) == 0 {
		return nil
	}
	out := make([]Filter, len(b.filters))
	copy(out, b.filters)
	return out
}

// Apply 应用到统一查询请求。
func (b *FilterBuilder) Apply(request *QueryRequest) {
	if request == nil || len(b.filters) == 0 {
		return
	}
	request.Filters = request.Filters.Merge(DecodeAdapterFilters(b.Build()))
}

// DecodeAdapterFilters 将适配层字符串过滤条件提升为“默认 string 语义”的查询表达式集合。
func DecodeAdapterFilters(filters []Filter) QueryFilters {
	if len(filters) == 0 {
		return nil
	}
	var out QueryFilters
	for _, filter := range filters {
		expr := QueryExpr{Op: filter.Op}
		field := strings.TrimSpace(filter.Field)
		if field == "" {
			continue
		}
		if filter.Op == FilterOpIn || filter.Op == FilterOpNotIn {
			expr.Values = make([]QueryValue, 0, len(filter.Values))
			for _, value := range filter.Values {
				expr.Values = append(expr.Values, QueryValue{
					Type:       FieldTypeString,
					Normalized: value,
					String:     value,
				})
			}
		} else {
			expr.Value = QueryValue{
				Type:       FieldTypeString,
				Normalized: filter.Value,
				String:     filter.Value,
			}
		}
		out = out.Append(field, expr)
	}
	if out.IsZero() {
		return nil
	}
	return out
}
