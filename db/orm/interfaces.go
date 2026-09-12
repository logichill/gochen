package orm

import (
	"context"
	"database/sql"

	"gochen/db"
	"gochen/db/dialect"
)

// IOrm 表示 ORM 适配器入口。
// 仅定义接口，具体实现由业务侧选择并以适配器形式注入。
type IOrm interface {
	// Capabilities 返回适配器支持的能力集合。
	Capabilities() Capabilities
	// WithContext 派生绑定上下文的 Orm 会话。
	WithContext(ctx context.Context) IOrm
	// Model 返回指定模型的操作入口。
	Model(meta *ModelMeta) (IModel, error)
	// Begin 开启事务会话。
	Begin(ctx context.Context) (IOrmSession, error)
	// BeginTx 开启带选项的事务会话。
	BeginTx(ctx context.Context, opts *sql.TxOptions) (IOrmSession, error)
	// Database 返回适配器绑定的通用数据库（可选，可为 nil）。
	Database() db.IDatabase
}

// IOrmSession 表示事务会话。
type IOrmSession interface {
	IOrm
	Commit() error
	Rollback() error
}

// IModel 封装模型级别的基础操作。
type IModel interface {
	Meta() *ModelMeta
	Capabilities() Capabilities
	// Dialect 返回模型绑定数据库的方言能力，必须为非 nil。
	Dialect() dialect.IDialect

	First(ctx context.Context, dest any, opts ...QueryOption) error
	Find(ctx context.Context, dest any, opts ...QueryOption) error
	// Count 统计满足条件的记录数。
	// 当指定 WithGroupBy 时，统计分组后的组数（即以子查询形式执行 SELECT COUNT(*) FROM (SELECT ... GROUP BY ...)）；
	// 仅应用 Where、Joins 和 GroupBy，忽略分页、投影、排序、预加载及行锁选项。
	Count(ctx context.Context, opts ...QueryOption) (int64, error)

	Create(ctx context.Context, entities ...any) error
	// Save 根据 QueryOptions 执行更新，通常结合主键或条件。
	Save(ctx context.Context, entity any, opts ...QueryOption) error
	UpdateValues(ctx context.Context, values map[string]any, opts ...QueryOption) error
	Delete(ctx context.Context, opts ...QueryOption) error

	Association(owner any, name string) IAssociation
}

// IModelWithResult 抽象模型并带结果能力接口。
type IModelWithResult interface {
	SaveWithResult(ctx context.Context, entity any, opts ...QueryOption) (sql.Result, error)
	UpdateValuesWithResult(ctx context.Context, values map[string]any, opts ...QueryOption) (sql.Result, error)
	DeleteWithResult(ctx context.Context, opts ...QueryOption) (sql.Result, error)
}

// IAssociation 表示关联维护的最小能力集。
type IAssociation interface {
	Name() string
	Owner() any
	Append(ctx context.Context, targets ...any) error
	Replace(ctx context.Context, targets ...any) error
	Delete(ctx context.Context, targets ...any) error
	Clear(ctx context.Context) error
}
