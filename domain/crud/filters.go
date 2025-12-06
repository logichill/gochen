package crud

// FilterBuilder 提供类型安全的过滤条件构建器，用于填充 QueryOptions.Filters。
//
// 设计目标：
//   - 通过显式方法（Eq/Like/Gt 等）表达常见条件，避免直接在调用方手写 magic key；
//   - 内部仍使用 map[string]any 承载，保持与现有 IRepository/IQueryableRepository 接口完全兼容；
//   - 与 data/orm/repo 的 withFilters 约定对齐：通过字段后缀（_like/_gt 等）表达操作符。
type FilterBuilder struct {
	filters map[string]any
}

// NewFilterBuilder 创建新的过滤构建器。
func NewFilterBuilder() *FilterBuilder {
	return &FilterBuilder{
		filters: make(map[string]any),
	}
}

// Eq 等值匹配：field = value
func (b *FilterBuilder) Eq(field string, value any) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field] = value
	return b
}

// Like 模糊匹配：field_like = value，对应 SQL: field LIKE '%value%'.
func (b *FilterBuilder) Like(field string, value string) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field+"_like"] = value
	return b
}

// Gt 大于：field_gt = value
func (b *FilterBuilder) Gt(field string, value any) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field+"_gt"] = value
	return b
}

// Gte 大于等于：field_gte = value
func (b *FilterBuilder) Gte(field string, value any) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field+"_gte"] = value
	return b
}

// Lt 小于：field_lt = value
func (b *FilterBuilder) Lt(field string, value any) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field+"_lt"] = value
	return b
}

// Lte 小于等于：field_lte = value
func (b *FilterBuilder) Lte(field string, value any) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field+"_lte"] = value
	return b
}

// Ne 不等于：field_ne = value
func (b *FilterBuilder) Ne(field string, value any) *FilterBuilder {
	if field == "" {
		return b
	}
	b.filters[field+"_ne"] = value
	return b
}

// In IN 列表：field_in = "v1,v2,..."（交由底层适配器解析字符串并拆分）。
//
// 为保持与现有实现兼容，这里简单地将切片拼接为逗号分隔字符串。
func (b *FilterBuilder) In(field string, values []string) *FilterBuilder {
	if field == "" || len(values) == 0 {
		return b
	}
	b.filters[field+"_in"] = joinComma(values)
	return b
}

// NotIn NOT IN 列表：field_not_in = "v1,v2,..."
func (b *FilterBuilder) NotIn(field string, values []string) *FilterBuilder {
	if field == "" || len(values) == 0 {
		return b
	}
	b.filters[field+"_not_in"] = joinComma(values)
	return b
}

// Build 返回内部持有的 map 副本，便于直接赋值给 QueryOptions.Filters。
func (b *FilterBuilder) Build() map[string]any {
	if len(b.filters) == 0 {
		return nil
	}
	out := make(map[string]any, len(b.filters))
	for k, v := range b.filters {
		out[k] = v
	}
	return out
}

// Apply 将构建好的过滤条件合并到给定的 QueryOptions 中。
func (b *FilterBuilder) Apply(opts *QueryOptions) {
	if opts == nil {
		return
	}
	if opts.Filters == nil {
		opts.Filters = make(map[string]any)
	}
	for k, v := range b.filters {
		opts.Filters[k] = v
	}
}

// joinComma 辅助：将字符串切片按逗号拼接。
func joinComma(values []string) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	n := 0
	for _, v := range values {
		n += len(v)
	}
	// 预留逗号空间
	buf := make([]byte, 0, n+len(values)-1)
	for i, v := range values {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, v...)
	}
	return string(buf)
}

