// Package audited 提供“CRUD + 审计 / 软删 / 恢复”的应用模板。
//
// Application 与 crud.Application 共用同一执行骨架，但不嵌入 crud.Application：
// 软删、恢复、清洗与审计轨迹必须经过本包的构造校验，不能绕过。
package audited

import (
	"context"
	"strings"

	appcrud "gochen/app/crud"
	"gochen/app/internal/depcheck"
	"gochen/app/internal/execution"
	"gochen/app/query"
	"gochen/auth"
	domaudited "gochen/domain/audited"
	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
)

// Repository 是 audited 应用要求的仓储组合。
type Repository[T domaudited.IAuditedEntity[ID], ID comparable] interface {
	appcrud.Repository[T, ID]
	domaudited.IRestoreRepository[T, ID]
	domaudited.IDeletedQueryRepository[T, ID]
	domcrud.IPurgeRepository[T, ID]
}

// Application 编排带审计记录、软删除与恢复的 CRUD 用例。
type Application[T domaudited.IAuditedEntity[ID], ID comparable] struct {
	core *execution.Core[T, ID]
}

// NewApplication 创建 audited 应用实例。
//
// 约束：
//   - repo 必须提供读写、事务、恢复、已删查询与永久删除端口；
//   - auditStore 必须非空，并实现 IAuditQueryStore，且与仓储声明同一数据源。
func NewApplication[T domaudited.IAuditedEntity[ID], ID comparable](
	repo Repository[T, ID],
	auditStore domaudited.IAuditStore,
	options Options[T, ID],
) (*Application[T, ID], error) {
	if depcheck.IsNil(repo) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if depcheck.IsNil(auditStore) {
		return nil, errors.NewCode(errors.InvalidInput, "audit store cannot be nil")
	}
	audits, ok := any(auditStore).(domaudited.IAuditQueryStore)
	if !ok || depcheck.IsNil(audits) {
		return nil, errors.NewCode(errors.InvalidInput, "audit store must implement IAuditQueryStore")
	}
	defaults, err := standardActions(options.Resource, options.ActionPermissions)
	if err != nil {
		return nil, err
	}
	core, err := execution.New(execution.Options[T, ID]{
		Resource:   options.Resource,
		Authorizer: options.Authorizer,
		Actions:    options.Actions,
		Defaults:   defaults,
		Hooks:      options.Hooks,
		Validator:  options.Validator,
		Limits:     options.Limits,
		Projector:  options.Projector,
		Repo:       repo,
		Read:       repo,
		Tx:         repo,
		Audit:      auditStore,
		Audits:     audits,
		SoftDelete: true,
		Restore:    repo,
		Deleted:    repo,
		Purge:      repo,
	})
	if err != nil {
		return nil, err
	}
	return &Application[T, ID]{core: core}, nil
}

// standardActions 构造带审计/软删能力的标准动作，并按动作名覆盖权限码。
func standardActions(resource string, permissions map[string]string) ([]auth.Action, error) {
	actions := []auth.Action{
		{Name: execution.ActionRead, Kind: auth.KindRead},
		{Name: execution.ActionList, Kind: auth.KindList},
		{Name: execution.ActionCreate, Kind: auth.KindCreate},
		{Name: execution.ActionUpdate, Kind: auth.KindUpdate},
		{Name: execution.ActionDelete, Kind: auth.KindDelete},
		{Name: execution.ActionRestore, Kind: auth.KindUpdate},
		{Name: execution.ActionPurge, Kind: auth.KindDelete},
		{Name: execution.ActionAuditRead, Kind: auth.KindRead},
		{Name: execution.ActionListDeleted, Kind: auth.KindList},
	}
	known := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		known[action.Name] = struct{}{}
	}
	for name := range permissions {
		if _, ok := known[name]; !ok {
			return nil, errors.NewCode(errors.InvalidInput, "action permission override references an unknown action").
				WithContext("action", name)
		}
	}
	for i := range actions {
		if code := strings.TrimSpace(permissions[actions[i].Name]); code != "" {
			actions[i].Permission = code
		}
	}
	return actions, nil
}

// Descriptor 返回构造期冻结的资源动作清单。
func (a *Application[T, ID]) Descriptor() auth.Descriptor {
	return a.core.Descriptor()
}

// MaxPageSize 返回单页上限。
func (a *Application[T, ID]) MaxPageSize() int { return a.core.MaxPageSize() }

// MaxBulkSize 返回批量上限。
func (a *Application[T, ID]) MaxBulkSize() int { return a.core.MaxBulkSize() }

// UsesAllowAuthorizer 报告当前应用是否使用一律放行的授权器（D17 启动清单）。
func (a *Application[T, ID]) UsesAllowAuthorizer() bool { return a.core.UsesAllowAuthorizer() }

// Get 读取单个实体（已软删记录不可见）。
func (a *Application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	return a.core.Get(ctx, id)
}

// Exists 判断实体是否存在（已软删记录不可见）。
func (a *Application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	return a.core.Exists(ctx, id)
}

// List 分页读取实体列表（已软删记录不可见）。
func (a *Application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	return a.core.List(ctx, offset, limit)
}

// Query 以统一查询请求读取实体列表。
func (a *Application[T, ID]) Query(ctx context.Context, opts query.QueryOptions) ([]T, error) {
	return a.core.Query(ctx, opts)
}

// Count 统计当前可见实体总数。
func (a *Application[T, ID]) Count(ctx context.Context) (int64, error) {
	return a.core.Count(ctx)
}

// QueryCount 统计符合查询条件的可见实体。
func (a *Application[T, ID]) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	return a.core.QueryCount(ctx, opts)
}

// Read 执行自定义读用例。
func (a *Application[T, ID]) Read(
	ctx context.Context,
	action string,
	fn func(ctx context.Context, s spec.Spec) error,
) error {
	return a.core.Read(ctx, action, fn)
}

// Create 创建实体并写入审计。
func (a *Application[T, ID]) Create(ctx context.Context, entity T) error {
	return a.core.Create(ctx, entity)
}

// Update 更新实体并写入审计差异。
func (a *Application[T, ID]) Update(ctx context.Context, entity T) error {
	return a.core.Update(ctx, entity)
}

// Delete 软删除实体并写入审计。
func (a *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	return a.core.Delete(ctx, id)
}

// DeleteBulk 一次授权、一个事务软删除多个实体。
func (a *Application[T, ID]) DeleteBulk(ctx context.Context, ids []ID) error {
	return a.core.DeleteBulk(ctx, ids)
}

// Execute 修改已有实体。
func (a *Application[T, ID]) Execute(
	ctx context.Context,
	action string,
	id ID,
	fn func(ctx context.Context, e T) error,
	opts ...ExecOption,
) (T, error) {
	return a.core.Execute(ctx, action, id, fn, opts...)
}

// ExecuteNew 新建实体。
func (a *Application[T, ID]) ExecuteNew(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) (T, error),
	opts ...ExecOption,
) (T, error) {
	return a.core.ExecuteNew(ctx, action, fn, opts...)
}

// ExecuteBulk 一次授权、一个事务修改多个实体。
func (a *Application[T, ID]) ExecuteBulk(
	ctx context.Context,
	action string,
	ids []ID,
	fn func(ctx context.Context, e T) error,
	opts ...ExecOption,
) ([]T, error) {
	return a.core.ExecuteBulk(ctx, action, ids, fn, opts...)
}

// ExecuteNewBulk 一次授权、一个事务新建多个实体。
func (a *Application[T, ID]) ExecuteNewBulk(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) ([]T, error),
	opts ...ExecOption,
) ([]T, error) {
	return a.core.ExecuteNewBulk(ctx, action, fn, opts...)
}

// Restore 恢复已软删实体并写入审计。
func (a *Application[T, ID]) Restore(ctx context.Context, id ID, opts ...ExecOption) (T, error) {
	return a.core.Restore(ctx, id, opts...)
}

// Purge 永久删除实体（含已软删记录）并写入审计。
func (a *Application[T, ID]) Purge(ctx context.Context, id ID, opts ...ExecOption) error {
	return a.core.Purge(ctx, id, opts...)
}

// ListDeleted 分页读取已软删实体。
func (a *Application[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	return a.core.ListDeleted(ctx, offset, limit)
}

// AuditTrail 返回实体的审计轨迹；实体被 Purge 后仍可读。
func (a *Application[T, ID]) AuditTrail(
	ctx context.Context,
	id ID,
	offset, limit int,
) ([]domaudited.AuditRecord, error) {
	return a.core.AuditTrail(ctx, id, offset, limit)
}
