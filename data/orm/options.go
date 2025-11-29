package orm

// Condition 表示基础查询条件，Expr 使用占位符 ?，Args 对应参数列表。
type Condition struct {
	Expr string
	Args []any
}

// OrderBy 表示排序字段。
type OrderBy struct {
	Column string
	Desc   bool
}

// QueryOptions 描述查询/更新的通用选项。
type QueryOptions struct {
	Where     []Condition
	Joins     []Join
	OrderBy   []OrderBy
	GroupBy   []string
	Limit     int
	Offset    int
	Select    []string
	Preload   []string
	ForUpdate bool
}

// QueryOption 用于配置 QueryOptions。
type QueryOption func(*QueryOptions)

// WithWhere 追加查询条件。
func WithWhere(expr string, args ...any) QueryOption {
	return func(opts *QueryOptions) {
		if expr == "" {
			return
		}
		opts.Where = append(opts.Where, Condition{Expr: expr, Args: args})
	}
}

// Join 表示查询关联。
type Join struct {
	Expr string
	Args []any
}

// WithJoin 追加 JOIN 片段。
func WithJoin(expr string, args ...any) QueryOption {
	return func(opts *QueryOptions) {
		if expr == "" {
			return
		}
		opts.Joins = append(opts.Joins, Join{Expr: expr, Args: args})
	}
}

// WithGroupBy 追加分组字段。
func WithGroupBy(columns ...string) QueryOption {
	return func(opts *QueryOptions) {
		if len(columns) == 0 {
			return
		}
		opts.GroupBy = append(opts.GroupBy, columns...)
	}
}

// WithOrderBy 追加排序。
func WithOrderBy(column string, desc bool) QueryOption {
	return func(opts *QueryOptions) {
		if column == "" {
			return
		}
		opts.OrderBy = append(opts.OrderBy, OrderBy{Column: column, Desc: desc})
	}
}

// WithLimit 设置查询条数上限。
func WithLimit(limit int) QueryOption {
	return func(opts *QueryOptions) {
		if limit > 0 {
			opts.Limit = limit
		}
	}
}

// WithOffset 设置查询偏移。
func WithOffset(offset int) QueryOption {
	return func(opts *QueryOptions) {
		if offset > 0 {
			opts.Offset = offset
		}
	}
}

// WithSelect 指定返回列。
func WithSelect(columns ...string) QueryOption {
	return func(opts *QueryOptions) {
		if len(columns) == 0 {
			return
		}
		opts.Select = append(opts.Select, columns...)
	}
}

// WithPreload 追加预加载关联。
func WithPreload(relations ...string) QueryOption {
	return func(opts *QueryOptions) {
		if len(relations) == 0 {
			return
		}
		opts.Preload = append(opts.Preload, relations...)
	}
}

// WithForUpdate 标记需要行级锁。
func WithForUpdate() QueryOption {
	return func(opts *QueryOptions) {
		opts.ForUpdate = true
	}
}

// CollectQueryOptions 聚合 QueryOption，方便适配器读取。
func CollectQueryOptions(options ...QueryOption) QueryOptions {
	var opts QueryOptions
	for _, opt := range options {
		if opt != nil {
			opt(&opts)
		}
	}
	return opts
}
