// Package crud 提供 CRUD 应用服务模板。
//
// Application 与 ReadApplication 按读写能力提供业务入口，内部执行骨架统一编排授权等语义。
package crud

import (
	"strings"

	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	domcrud "gochen/domain/crud"

	"gochen/errors"
	"gochen/internal/depcheck"
)

// Application 编排通用 CRUD 用例。
type Application[T domain.IEntity[ID], ID comparable] struct {
	readApplication[T, ID]
}

// NewApplication 创建可读写的 CRUD 应用。
//
// 标准动作由构造期删除配置确定；配置 Options.Audit 时
// 只参与写入协议；历史查询由 app/crud/audit.Reader 提供。
func NewApplication[T domain.IEntity[ID], ID comparable](
	repo Repository[T, ID],
	options Options[T, ID],
) (*Application[T, ID], error) {
	if depcheck.IsNil(repo) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if err := validateOptions(options, true); err != nil {
		return nil, err
	}
	defaults, err := standardActions(options.Resource, options.ActionPermissions, true, options.DeleteMode == DeleteSoft, options.EnableRestore, options.EnableDeletedQuery)
	if err != nil {
		return nil, err
	}
	purge, ok := repo.(domcrud.IPurgeRepository[T, ID])
	if !ok || depcheck.IsNil(purge) {
		return nil, errors.NewCode(errors.InvalidInput, "repository must provide physical purge capability")
	}
	deletedRead, _ := repo.(domcrud.IDeletedReadRepository[T, ID])
	if isSoftDeletable[T, ID]() && depcheck.IsNil(deletedRead) {
		return nil, errors.NewCode(errors.InvalidInput, "soft-deletable entity requires including-deleted read capability for purge")
	}
	var restore domcrud.IRestoreRepository[T, ID]
	if options.EnableRestore {
		restore, _ = repo.(domcrud.IRestoreRepository[T, ID])
		if depcheck.IsNil(restore) {
			return nil, errors.NewCode(errors.InvalidInput, "restore capability is required")
		}
	}
	var deleted domcrud.IDeletedQueryRepository[T, ID]
	if options.EnableDeletedQuery {
		deleted, _ = repo.(domcrud.IDeletedQueryRepository[T, ID])
		if depcheck.IsNil(deleted) {
			return nil, errors.NewCode(errors.InvalidInput, "deleted query capability is required")
		}
	}
	core, err := execution.New(execution.Options[T, ID]{
		SoftDelete:  options.DeleteMode == DeleteSoft,
		DeletedRead: deletedRead,
		Restore:     restore,
		Deleted:     deleted,
		Purge:       purge,
		Resource:    options.Resource,
		Authorizer:  options.Authorizer,
		Actions:     options.Actions,
		Defaults:    defaults,
		Hooks:       options.Hooks,
		Validator:   options.Validator,
		Limits:      options.Limits,
		Repo:        repo,
		Read:        repo,
		Tx:          repo,
		Audit:       options.Audit,
		Projector:   options.Projector,
	})
	if err != nil {
		return nil, err
	}
	return &Application[T, ID]{readApplication: readApplication[T, ID]{core: core}}, nil
}

// standardActions 构造标准动作清单；permissions 按动作名覆盖默认权限码。
func standardActions(resource string, permissions map[string]string, writable, softDelete, restore, deletedQuery bool) ([]auth.Action, error) {
	resource = strings.TrimSpace(resource)
	names := []string{ActionRead, ActionList}
	if writable {
		names = append(names, ActionCreate, ActionUpdate)
		if softDelete {
			names = append(names, ActionDelete)
		}
		names = append(names, ActionPurge)
		if restore {
			names = append(names, ActionRestore)
		}
		if deletedQuery {
			names = append(names, ActionListDeleted)
		}
	}
	known := make(map[string]struct{}, len(names))
	for _, name := range names {
		known[name] = struct{}{}
	}
	for name := range permissions {
		if _, ok := known[name]; !ok {
			return nil, errors.NewCode(errors.InvalidInput, "action permission override references an unknown action").
				WithContext("action", name)
		}
	}

	permissionFor := func(name string) string {
		if code := strings.TrimSpace(permissions[name]); code != "" {
			return code
		}
		if resource == "" {
			return ""
		}
		return resource + ":api:" + name
	}

	actions := make([]auth.Action, 0, len(names))
	for _, name := range names {
		kind := auth.KindRead
		switch name {
		case ActionList, ActionListDeleted:
			kind = auth.KindList
		case ActionCreate:
			kind = auth.KindCreate
		case ActionUpdate, ActionRestore:
			kind = auth.KindUpdate
		case ActionDelete, ActionPurge:
			kind = auth.KindDelete
		}
		actions = append(actions, auth.Action{
			Name:       name,
			Permission: permissionFor(name),
			Kind:       kind,
		})
	}
	return actions, nil
}

func isSoftDeletable[T domain.IEntity[ID], ID comparable]() bool {
	var zero T
	_, ok := any(zero).(domain.ISoftDeletable)
	return ok
}

// MaxBulkSize 返回批量上限。
func (a *Application[T, ID]) MaxBulkSize() int { return a.core.MaxBulkSize() }

func validateOptions[T domain.IEntity[ID], ID comparable](options Options[T, ID], writable bool) error {
	if writable && options.DeleteMode != DeleteSoft && options.DeleteMode != DeletePurge {
		return errors.NewCode(errors.InvalidInput, "write application requires explicit DeleteMode (DeleteSoft or DeletePurge)")
	}
	if !writable && (options.DeleteMode != 0 || options.EnableRestore || options.EnableDeletedQuery) {
		return errors.NewCode(errors.InvalidInput, "read-only application cannot configure deletion lifecycle")
	}
	if (options.DeleteMode == DeleteSoft || options.EnableRestore || options.EnableDeletedQuery) && !isSoftDeletable[T, ID]() {
		return errors.NewCode(errors.InvalidInput, "deletion state capabilities require domain.ISoftDeletable")
	}
	for _, action := range options.Actions {
		switch strings.TrimSpace(action.Name) {
		case ActionDelete, ActionRestore, ActionPurge, ActionListDeleted, execution.ActionAuditRead:
			return errors.NewCode(errors.InvalidInput, "built-in lifecycle and audit actions cannot be declared as custom actions").WithContext("action", action.Name)
		}
		if !writable && action.Kind != auth.KindRead && action.Kind != auth.KindList {
			return errors.NewCode(errors.InvalidInput, "read-only application cannot declare write actions")
		}
	}
	for name := range options.Hooks {
		if strings.TrimSpace(name) != name {
			return errors.NewCode(errors.InvalidInput, "hook action name must be canonical").WithContext("action", name)
		}
		switch name {
		case ActionDelete:
			if !writable || options.DeleteMode != DeleteSoft {
				return errors.NewCode(errors.InvalidInput, "delete hooks require soft-delete mode; use purge hooks for hard deletion")
			}
		case ActionRestore:
			if !writable || !options.EnableRestore {
				return errors.NewCode(errors.InvalidInput, "restore hooks require restore capability")
			}
		case ActionPurge:
			if !writable {
				return errors.NewCode(errors.InvalidInput, "read-only application cannot configure purge hooks")
			}
		case ActionListDeleted:
			if !writable || !options.EnableDeletedQuery {
				return errors.NewCode(errors.InvalidInput, "deleted-query hooks require deleted-query capability")
			}
		}
	}
	return nil
}
