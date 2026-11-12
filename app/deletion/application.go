// Package deletion 提供独立可选的软删除、恢复、永久清理与回收站查询。
package deletion

import (
	"context"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"strings"

	appcrud "gochen/app/crud"
	"gochen/app/internal/execution"

	"gochen/auth"

	"gochen/errors"
	"gochen/internal/depcheck"
)

// IEntity 是删除用例所需的最小实体状态。
type IEntity[ID comparable] interface {
	domain.IEntity[ID]
	domain.ISoftDeletable
}

// Repository 是删除生命周期要求的条件写入与状态查询能力。
type Repository[T IEntity[ID], ID comparable] interface {
	appcrud.Repository[T, ID]
	domcrud.IRestoreRepository[T, ID]
	domcrud.IDeletedQueryRepository[T, ID]
	domcrud.IPurgeRepository[T, ID]
}

// Application 编排删除生命周期，可选择参与同源事务的审计写入。
type Application[T IEntity[ID], ID comparable] struct {
	core *execution.Core[T, ID]
}

// NewApplication 创建删除生命周期应用。
//
// 约束：
//   - repo 必须提供读写、事务、恢复、已删查询与永久删除端口；
//   - 配置 Audit 时，它必须与仓储声明并参与同一数据源事务。
func NewApplication[T IEntity[ID], ID comparable](
	repo Repository[T, ID],

	options Options[T, ID],
) (*Application[T, ID], error) {
	if depcheck.IsNil(repo) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if len(options.Actions) != 0 {
		return nil, errors.NewCode(errors.InvalidInput, "deletion application does not expose custom actions")
	}
	defaults, err := standardActions(options.Resource, options.ActionPermissions)
	if err != nil {
		return nil, err
	}
	core, err := execution.New(execution.Options[T, ID]{
		CommandGrant: options.CommandGrant,
		Resource:     options.Resource,
		Authorizer:   options.Authorizer,
		Actions:      options.Actions,
		Defaults:     defaults,
		Hooks:        options.Hooks,
		Validator:    options.Validator,
		Limits:       options.Limits,
		Projector:    options.Projector,
		Repo:         repo,
		Read:         repo,
		Tx:           repo,
		Audit:        options.Audit,
		SoftDelete:   true,
		Restore:      repo,
		Deleted:      repo,
		Purge:        repo,
	})
	if err != nil {
		return nil, err
	}
	return &Application[T, ID]{core: core}, nil
}

// standardActions 构造删除生命周期的标准动作，并按动作名覆盖权限码。
func standardActions(resource string, permissions map[string]string) ([]auth.Action, error) {
	actions := []auth.Action{
		{Name: execution.ActionDelete, Kind: auth.KindDelete},
		{Name: execution.ActionRestore, Kind: auth.KindUpdate},
		{Name: execution.ActionPurge, Kind: auth.KindDelete},
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

// Delete 软删除实体并写入审计。
func (a *Application[T, ID]) Delete(ctx context.Context, id ID) error {
	return a.core.Delete(ctx, id)
}

// DeleteBulk 一次授权、一个事务软删除多个实体。
func (a *Application[T, ID]) DeleteBulk(ctx context.Context, ids []ID) error {
	return a.core.DeleteBulk(ctx, ids)
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
