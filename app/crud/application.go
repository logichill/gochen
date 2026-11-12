// Package crud 提供 CRUD 应用服务模板。
//
// Application 与 ReadApplication 按读写能力提供业务入口，内部执行骨架统一编排授权等语义。
package crud

import (
	"strings"

	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// Application 编排通用 CRUD 用例。
type Application[T domain.IEntity[ID], ID comparable] struct {
	readApplication[T, ID]
}

// NewApplication 创建可读写的 CRUD 应用。
//
// 标准动作：read / list / create / update / delete；配置 Options.Audit 时
// 另加 audit_read，并提供 AuditTrail。
func NewApplication[T domain.IEntity[ID], ID comparable](
	repo Repository[T, ID],
	options Options[T, ID],
) (*Application[T, ID], error) {
	if depcheck.IsNil(repo) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	audits := options.Audit
	if err := rejectUndeclaredAudit(options.Actions, audits); err != nil {
		return nil, err
	}
	defaults, err := standardActions(options.Resource, options.ActionPermissions, true, !depcheck.IsNil(audits))
	if err != nil {
		return nil, err
	}
	core, err := execution.New(execution.Options[T, ID]{
		Resource:     options.Resource,
		Authorizer:   options.Authorizer,
		CommandGrant: options.CommandGrant,
		Actions:      options.Actions,
		Defaults:     defaults,
		Hooks:        options.Hooks,
		Validator:    options.Validator,
		Limits:       options.Limits,
		Repo:         repo,
		Read:         repo,
		Tx:           repo,
		Audit:        options.Audit,
	})
	if err != nil {
		return nil, err
	}
	return &Application[T, ID]{readApplication: readApplication[T, ID]{core: core}}, nil
}

// standardActions 构造标准动作清单；permissions 按动作名覆盖默认权限码。
func standardActions(resource string, permissions map[string]string, writable, withAudit bool) ([]auth.Action, error) {
	resource = strings.TrimSpace(resource)
	names := []string{ActionRead, ActionList}
	if writable {
		names = append(names, ActionCreate, ActionUpdate, ActionDelete)
	}
	if withAudit {
		names = append(names, ActionAuditRead)
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
		case ActionList:
			kind = auth.KindList
		case ActionCreate:
			kind = auth.KindCreate
		case ActionUpdate:
			kind = auth.KindUpdate
		case ActionDelete:
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

func rejectUndeclaredAudit(actions []auth.Action, store domaudited.IAuditQueryStore) error {
	if !depcheck.IsNil(store) {
		return nil
	}
	for _, action := range actions {
		if action.Name == ActionAuditRead {
			return errors.NewCode(errors.InvalidInput, "audit_read requires an audit store")
		}
	}
	return nil
}

// MaxBulkSize 返回批量上限。
func (a *Application[T, ID]) MaxBulkSize() int { return a.core.MaxBulkSize() }
