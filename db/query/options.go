package query

import (
	"context"
	"strings"
)

// QueryExpr 表示一个字段上的单条查询表达式。
type QueryExpr struct {
	Op     FilterOp
	Value  QueryValue
	Values []QueryValue
}

// QueryFilters 表示按字段归组后的查询表达式集合。
type QueryFilters map[string][]QueryExpr

func cloneQueryExpr(expr QueryExpr) QueryExpr {
	cloned := expr
	if len(expr.Values) > 0 {
		cloned.Values = append([]QueryValue(nil), expr.Values...)
	}
	return cloned
}

func cloneQueryExprs(exprs []QueryExpr) []QueryExpr {
	if len(exprs) == 0 {
		return nil
	}
	out := make([]QueryExpr, len(exprs))
	for i, expr := range exprs {
		out[i] = cloneQueryExpr(expr)
	}
	return out
}

// IsZero 判断是否为空过滤集合。
func (f QueryFilters) IsZero() bool {
	for _, exprs := range f {
		if len(exprs) > 0 {
			return false
		}
	}
	return true
}

// Has 判断指定字段是否存在过滤表达式。
func (f QueryFilters) Has(field string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return false
	}
	return len(f[field]) > 0
}

func (f QueryFilters) Get(field string) []QueryExpr {
	field = strings.TrimSpace(field)
	if field == "" || len(f[field]) == 0 {
		return nil
	}
	return cloneQueryExprs(f[field])
}

func (f QueryFilters) First(field string) (QueryExpr, bool) {
	items := f.Get(field)
	if len(items) == 0 {
		return QueryExpr{}, false
	}
	return items[0], true
}

// Clone 返回深拷贝，避免外部后续修改影响调用方。
func (f QueryFilters) Clone() QueryFilters {
	if len(f) == 0 {
		return nil
	}
	out := make(QueryFilters, len(f))
	for field, exprs := range f {
		if len(exprs) == 0 {
			continue
		}
		out[field] = cloneQueryExprs(exprs)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Append 追加单条过滤表达式。
func (f QueryFilters) Append(field string, expr QueryExpr) QueryFilters {
	field = strings.TrimSpace(field)
	if field == "" {
		return f
	}
	if f == nil {
		f = make(QueryFilters)
	}
	f[field] = append(f[field], cloneQueryExpr(expr))
	return f
}

// Merge 追加另一组过滤表达式。
func (f QueryFilters) Merge(other QueryFilters) QueryFilters {
	if other.IsZero() {
		return f
	}
	if f == nil {
		f = make(QueryFilters, len(other))
	}
	for field, exprs := range other {
		if len(exprs) == 0 {
			continue
		}
		f[field] = append(f[field], cloneQueryExprs(exprs)...)
	}
	return f
}

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
type IQueryableRepository[T any, ID comparable] interface {
	Create(ctx context.Context, e T) error
	Update(ctx context.Context, e T) error
	Delete(ctx context.Context, id ID) error
	Get(ctx context.Context, id ID) (T, error)
	List(ctx context.Context, offset, limit int) ([]T, error)
	Count(ctx context.Context) (int64, error)
	Exists(ctx context.Context, id ID) (bool, error)

	// Query 通用查询。
	Query(ctx context.Context, opts QueryOptions) ([]T, error)

	// QueryOne 查询单条记录。
	QueryOne(ctx context.Context, opts QueryOptions) (T, error)

	// QueryCount 查询计数。
	QueryCount(ctx context.Context, opts QueryOptions) (int64, error)
}
