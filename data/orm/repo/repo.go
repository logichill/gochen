package repo

import (
	"context"

	"gochen/data/orm"
	sentity "gochen/domain/entity"
)

// Repo 基于 gochen/data/orm 的通用仓储实现。
// 约束：实体需实现 IObject[int64] 与 IValidatable。
type Repo[T interface {
	sentity.IObject[int64]
	sentity.IValidatable
}] struct {
	orm   orm.IOrm
	model orm.IModel
}

// NewRepo 创建基础仓储实例。
func NewRepo[T interface {
	sentity.IObject[int64]
	sentity.IValidatable
}](ormEngine orm.IOrm, tableName string) *Repo[T] {
	meta := &orm.ModelMeta{
		Model: new(T),
		Table: tableName,
	}
	return &Repo[T]{
		orm:   ormEngine,
		model: ormEngine.Model(meta),
	}
}

func (r *Repo[T]) query(ctx context.Context) *queryBuilder {
	return newQueryBuilder(r.model, ctx)
}

// Model 暴露底层模型，供子类进行关联操作。
func (r *Repo[T]) Model() orm.IModel { return r.model }

// Orm 返回绑定的 ORM 引擎。
func (r *Repo[T]) Orm() orm.IOrm { return r.orm }

// Association 返回指定实体的关联操作入口。
func (r *Repo[T]) Association(owner any, name string) orm.IAssociation {
	return r.model.Association(owner, name)
}
