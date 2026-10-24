package query

import (
	"context"
)

// QueryRequest 表示 application / repository 主路径消费的统一查询请求。
type QueryRequest struct {
	Fields  []string     `json:"fields"`
	Sorts   []Sort       `json:"sorts"`
	Filters QueryFilters `json:"filters"`
}

// IsZero 判断是否为空查询请求。
func (q QueryRequest) IsZero() bool {
	return len(q.Fields) == 0 && len(q.Sorts) == 0 && q.Filters.IsZero()
}

func (q QueryRequest) Clone() QueryRequest {
	return QueryRequest{
		Fields:  append([]string(nil), q.Fields...),
		Sorts:   append([]Sort(nil), q.Sorts...),
		Filters: q.Filters.Clone(),
	}
}

// QueryOptions 查询选项（偏移窗口 + 统一查询请求）。
type QueryOptions struct {
	// Offset 偏移量。
	Offset int

	// Limit 每页数量。
	Limit int

	// Fields 表示返回字段投影。
	Fields []string

	// Sorts 表示排序规则。
	Sorts []Sort

	// Filters 表示按字段归组后的过滤表达式。
	Filters QueryFilters

	// Advanced 表示受控高级过滤条件。
	Advanced AdvancedFilters
}

// Request 返回 QueryOptions 对应的查询请求视图。
func (q QueryOptions) Request() QueryRequest {
	return QueryRequest{
		Fields:  append([]string(nil), q.Fields...),
		Sorts:   append([]Sort(nil), q.Sorts...),
		Filters: q.Filters.Clone(),
	}
}

// IQueryableRepository 可查询仓储接口（可选扩展端口）。
//
// # 错误契约。
//
//   - Query: 空结果返回空 slice + nil（不返回 NotFound）
//   - QueryOne: 未找到返回 errors.NotFound
//   - QueryCount: 空结果返回 0 + nil
//
// 说明：
// - 该接口直接消费统一 QueryRequest / QueryOptions；
// - HTTP/filter DSL 只在 adapter 层出现，进入 application 后应已收口为 QueryFilters。
// - 需要基础 CRUD 能力的调用方应与领域仓储端口显式组合。
type IQueryableRepository[T any] interface {
	// Query 通用查询。
	Query(ctx context.Context, opts QueryOptions) ([]T, error)

	// QueryOne 查询单条记录。
	QueryOne(ctx context.Context, opts QueryOptions) (T, error)

	// QueryCount 查询计数。
	QueryCount(ctx context.Context, opts QueryOptions) (int64, error)
}
