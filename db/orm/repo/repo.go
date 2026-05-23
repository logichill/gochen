package repo

import (
	"context"

	"gochen/db/orm"
	"gochen/domain"
	"gochen/domain/access"
	"gochen/domain/audited"
	"gochen/errors"
	"gochen/ident"
)

// Repo 基于 gochen/db/orm 的通用仓储实现。
//
// 类型参数：
//   - T: 实体类型，需实现 domain.IEntity[ID]
//   - ID: 主键类型，需满足 comparable 约束（如 int64、string、uuid.UUID 等）
type Repo[T domain.IEntity[ID], ID comparable] struct {
	orm            orm.IOrm
	model          orm.IModel
	meta           *orm.ModelMeta
	idGen          ident.IGenerator[ID]
	resourceKind   string
	accessColumns  accessColumns
	dataScope      access.IDataScopeResolver
	softDelete     bool
	auditFields    bool
	defaultActor   string
	softDeleteCols softDeleteColumns
}

// NewRepo 创建仓储。
func NewRepo[T domain.IEntity[ID], ID comparable](ormEngine orm.IOrm, tableName string, opts ...Option[T, ID]) (*Repo[T, ID], error) {
	if session, ok := ormEngine.(orm.IOrmSession); ok && session != nil {
		if _, ok := orm.AfterCommitDispatcherFromSession(session); !ok {
			return nil, errors.NewCode(errors.Unsupported,
				"session-bound repo requires an after-commit-aware orm session to propagate post-commit callbacks",
			)
		}
	}

	meta := &orm.ModelMeta{
		ModelFactory: orm.NewModelFactory[T](),
		Table:        tableName,
	}
	r := &Repo[T, ID]{
		orm:          ormEngine,
		meta:         meta,
		resourceKind: tableName,
		dataScope:    access.ContextDataScopeResolver{},
		defaultActor: "system",
	}

	// 能力探测（不依赖具体实体实例值）
	var zero T
	if _, ok := any(zero).(domain.ISoftDeletable); ok {
		r.softDelete = true
		r.softDeleteCols = softDeleteColumns{
			DeletedAt: "deleted_at",
			DeletedBy: "",
		}
	}
	if _, ok := any(zero).(audited.IAuditable); ok {
		r.auditFields = true
		// audited 场景下默认补齐 deleted_by；轻量 soft delete 可通过 WithSoftDeleteColumns 显式配置。
		if r.softDelete && r.softDeleteCols.DeletedBy == "" {
			r.softDeleteCols.DeletedBy = "deleted_by"
		}
	}

	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	if err := validateSemanticColumnTags[T](); err != nil {
		return nil, err
	}
	if err := r.validateColumnIdentifiers(); err != nil {
		return nil, err
	}

	model, err := ormEngine.Model(meta)
	if err != nil {
		return nil, err
	}
	r.model = model
	return r, nil
}

func (r *Repo[T, ID]) query(ctx context.Context) (*queryBuilder, error) {
	model, err := r.ModelFor(ctx)
	if err != nil {
		return nil, err
	}
	return r.applyDataScope(newQueryBuilder(model, ctx))
}

func (r *Repo[T, ID]) ModelFor(ctx context.Context) (orm.IModel, error) {
	if session, ok := orm.SessionFromContext(ctx); ok && session != nil {
		return session.Model(r.meta)
	}
	return r.model, nil
}

// Model 暴露底层模型，供子类进行关联操作。
func (r *Repo[T, ID]) Model() orm.IModel { return r.model }

// Orm 返回绑定的 ORM 引擎。
func (r *Repo[T, ID]) Orm() orm.IOrm { return r.orm }

func (r *Repo[T, ID]) Association(owner any, name string) orm.IAssociation {
	return r.model.Association(owner, name)
}

func (r *Repo[T, ID]) withOrm(ormEngine orm.IOrm) (*Repo[T, ID], error) {
	cp := *r
	cp.orm = ormEngine
	model, err := ormEngine.Model(r.meta)
	if err != nil {
		return nil, err
	}
	cp.model = model
	return &cp, nil
}

// ValidateWriteConstraintSupport 预检 constrained update/delete 所需的 ORM result 能力。
func (r *Repo[T, ID]) ValidateWriteConstraintSupport() error {
	if r == nil || r.model == nil {
		return errors.NewCode(errors.InvalidInput, "repo model is not initialized")
	}
	if _, ok := r.model.(orm.IModelWithResult); ok {
		return nil
	}
	return errors.NewCode(errors.Unsupported, "constrained update/delete requires an orm model with result support").
		WithContext("resource_kind", r.writeResourceKind())
}

type softDeleteColumns struct {
	DeletedAt string
	DeletedBy string
}
