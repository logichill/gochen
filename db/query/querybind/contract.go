package querybind

import "gochen/db/query"

// Contract 收敛同一 query input struct 的 schema 推导与绑定入口。
//
// 说明：
// - 单一输入 struct 同时作为 QuerySchema 推导源与 querybind 绑定目标；
// - 默认值与业务语义收口仍由调用方在本地 builder 中显式处理。
type Contract[T any] struct {
	schema *query.QuerySchema
	opts   *query.SchemaInferOptions
}

// NewContract 基于单一 query input struct 创建 contract。
func NewContract[T any](opts *query.SchemaInferOptions) (Contract[T], error) {
	schema, err := query.InferQuerySchema[T](opts)
	if err != nil {
		return Contract[T]{}, err
	}
	return Contract[T]{schema: schema, opts: cloneSchemaInferOptions(opts)}, nil
}

// MustNewContract 基于单一 query input struct 创建 contract，失败时直接 panic。
func MustNewContract[T any](opts *query.SchemaInferOptions) Contract[T] {
	contract, err := NewContract[T](opts)
	if err != nil {
		panic(err)
	}
	return contract
}

// Schema 返回 contract 对应的 QuerySchema。
func (c Contract[T]) Schema() *query.QuerySchema { return c.schema }

// Decode 绑定 QueryFilters 并返回输入 struct。
func (c Contract[T]) Decode(filters query.QueryFilters) (T, error) {
	return decodeWithSchemaInferOptions[T](filters, c.opts)
}

func cloneSchemaInferOptions(opts *query.SchemaInferOptions) *query.SchemaInferOptions {
	if opts == nil {
		return nil
	}
	cloned := *opts
	return &cloned
}
