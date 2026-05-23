package orm

// Condition 表示一段带参数的查询条件表达式。
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

// QueryOption 用于声明式构建 QueryOptions。
type QueryOption func(*QueryOptions)

// WithWhere 追加一段带参数的查询条件。
func WithWhere(expr string, args ...any) QueryOption {
	return func(opts *QueryOptions) {
		if expr == "" {
			return
		}
		opts.Where = append(opts.Where, Condition{Expr: expr, Args: args})
	}
}

// JoinType 表示 JOIN 类型。
type JoinType string

const (
	// JoinInner 表示 INNER JOIN。
	JoinInner JoinType = "INNER"
	// JoinLeft 表示 LEFT JOIN。
	JoinLeft JoinType = "LEFT"
	// JoinRight 表示 RIGHT JOIN。
	JoinRight JoinType = "RIGHT"
)

// JoinOn 表示一个 join 条件（仅支持等值连接）。
type JoinOn struct {
	Left  string
	Right string
}

// On 创建一个 JOIN 条件，表示两个字段的等值连接关系。
func On(left, right string) JoinOn { return JoinOn{Left: left, Right: right} }

// Join 描述一个结构化 JOIN 子句。
type Join struct {
	Type  JoinType
	Table string
	Alias string
	On    []JoinOn
}

// InnerJoin 创建一个内连接 JOIN 条件。
func InnerJoin(table, alias string, on ...JoinOn) Join {
	return Join{Type: JoinInner, Table: table, Alias: alias, On: on}
}

// LeftJoin 创建一个左连接 JOIN 条件。
func LeftJoin(table, alias string, on ...JoinOn) Join {
	return Join{Type: JoinLeft, Table: table, Alias: alias, On: on}
}

// RightJoin 创建一个右连接 JOIN 条件。
func RightJoin(table, alias string, on ...JoinOn) Join {
	return Join{Type: JoinRight, Table: table, Alias: alias, On: on}
}

// WithJoin 追加结构化 JOIN。
func WithJoin(j Join) QueryOption {
	return func(opts *QueryOptions) {
		opts.Joins = append(opts.Joins, j)
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

// WithOrderBy 追加一条排序规则。
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

// WithOffset 设置查询偏移量。
func WithOffset(offset int) QueryOption {
	return func(opts *QueryOptions) {
		if offset > 0 {
			opts.Offset = offset
		}
	}
}

// WithSelect 指定要返回的列集合。
func WithSelect(columns ...string) QueryOption {
	return func(opts *QueryOptions) {
		if len(columns) == 0 {
			return
		}
		opts.Select = append(opts.Select, columns...)
	}
}

// WithPreload 追加预加载关联名称。
func WithPreload(relations ...string) QueryOption {
	return func(opts *QueryOptions) {
		if len(relations) == 0 {
			return
		}
		opts.Preload = append(opts.Preload, relations...)
	}
}

// WithForUpdate 标记查询需要行级锁。
func WithForUpdate() QueryOption {
	return func(opts *QueryOptions) {
		opts.ForUpdate = true
	}
}

// CollectQueryOptions 按顺序应用所有 QueryOption 并返回最终结果。
func CollectQueryOptions(options ...QueryOption) QueryOptions {
	var opts QueryOptions
	for _, opt := range options {
		if opt != nil {
			opt(&opts)
		}
	}
	return opts
}
