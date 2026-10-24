package query

import (
	"strings"

	"gochen/errors"
)

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
	if b == nil {
		return nil
	}
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
	if b == nil {
		return nil
	}
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
	if b == nil {
		return nil
	}
	if len(values) == 0 {
		return b
	}
	return b.addValues(field, FilterOpIn, values)
}

func (b *FilterBuilder) NotIn(field string, values ...string) *FilterBuilder {
	if b == nil {
		return nil
	}
	if len(values) == 0 {
		return b
	}
	return b.addValues(field, FilterOpNotIn, values)
}

// IsNull 判断Null。
func (b *FilterBuilder) IsNull(field string) *FilterBuilder {
	if b == nil {
		return nil
	}
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
	if b == nil {
		return nil
	}
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
	if b == nil || len(b.filters) == 0 {
		return nil
	}
	out := make([]Filter, len(b.filters))
	copy(out, b.filters)
	return out
}

// Apply 应用到统一查询请求。
//
// 没有 QuerySchema 时无法做类型感知的范围比较，因此 gt/gte/lt/lte 会报错；
// 需要范围过滤请改用 ApplyWithSchema。
func (b *FilterBuilder) Apply(request *QueryRequest) error {
	if b == nil || request == nil || len(b.filters) == 0 {
		return nil
	}
	decoded, err := DecodeAdapterFilters(b.Build())
	if err != nil {
		return err
	}
	request.Filters = request.Filters.Merge(decoded)
	return nil
}

// ApplyWithSchema 应用带 QuerySchema 的过滤条件到统一查询请求。
//
// 与 Apply 的区别：范围操作符（gt/gte/lt/lte）需要字段类型才能解码，
// 有 schema 时按字段类型做类型感知解码，不再无条件拒绝。
func (b *FilterBuilder) ApplyWithSchema(request *QueryRequest, schema *QuerySchema) error {
	if b == nil || request == nil || len(b.filters) == 0 {
		return nil
	}
	if schema == nil {
		return errors.NewCode(errors.InvalidInput, "filter builder: schema is required for schema-aware apply")
	}
	decoded, err := schema.DecodeFilters(b.Build())
	if err != nil {
		return err
	}
	request.Filters = request.Filters.Merge(decoded)
	return nil
}
