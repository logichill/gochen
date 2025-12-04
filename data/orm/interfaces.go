package orm

import (
	"context"
	"database/sql"

	"gochen/data/db"
)

// IOrm 表示 ORM 适配器入口。
// 仅定义接口，具体实现由业务侧选择并以适配器形式注入。
type IOrm interface {
	// Capabilities 返回适配器支持的能力集合。
	Capabilities() Capabilities
	// WithContext 派生绑定上下文的 Orm 会话。
	WithContext(ctx context.Context) IOrm
	// Model 返回指定模型的操作入口。
	Model(meta *ModelMeta) IModel
	// Begin 开启事务会话。
	Begin(ctx context.Context) (IOrmSession, error)
	// BeginTx 开启带选项的事务会话。
	BeginTx(ctx context.Context, opts *sql.TxOptions) (IOrmSession, error)
	// Database 返回适配器绑定的通用数据库（可选，可为 nil）。
	Database() db.IDatabase
	// Raw 返回底层 ORM 引擎实例（例如 *gorm.DB），便于特殊场景透传。
	Raw() any
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

	First(ctx context.Context, dest any, opts ...QueryOption) error
	Find(ctx context.Context, dest any, opts ...QueryOption) error
	Count(ctx context.Context, opts ...QueryOption) (int64, error)

	Create(ctx context.Context, entities ...any) error
	// Save 根据 QueryOptions 执行更新，通常结合主键或条件。
	Save(ctx context.Context, entity any, opts ...QueryOption) error
	UpdateValues(ctx context.Context, values map[string]any, opts ...QueryOption) error
	Delete(ctx context.Context, opts ...QueryOption) error

	Association(owner any, name string) IAssociation
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
