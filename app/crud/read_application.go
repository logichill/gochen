package crud

import (
	"context"

	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// ReadApplication 编排只读用例，不提供写入、动作执行或归属迁移方法。
// 审计读取由独立的 app/crud/audit.Reader 提供。
type ReadApplication[T domain.IEntity[ID], ID comparable] struct {
	readApplication[T, ID]
}

// readApplication 共享读用例实现，执行核心只允许由构造函数绑定。
type readApplication[T domain.IEntity[ID], ID comparable] struct {
	core *execution.Core[T, ID]
}

// NewReadApplication 创建只读 CRUD 应用。
//
// 标准动作只有 read / list。
func NewReadApplication[T domain.IEntity[ID], ID comparable](
	repo IReadRepository[T, ID],
	options Options[T, ID],
) (*ReadApplication[T, ID], error) {
	if depcheck.IsNil(repo) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	for _, action := range options.Actions {
		if action.Name == execution.ActionAuditRead {
			return nil, errors.NewCode(errors.InvalidInput, "audit_read belongs to the independent audit reader")
		}
	}
	defaults, err := standardActions(options.Resource, options.ActionPermissions, false, canDelete[T, ID]())
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
		Read:       repo,
		Audit:      options.Audit,
		Projector:  options.Projector,
	})
	if err != nil {
		return nil, err
	}
	return &ReadApplication[T, ID]{readApplication: readApplication[T, ID]{core: core}}, nil
}

// Descriptor 返回构造期冻结的资源动作清单。
func (a *readApplication[T, ID]) Descriptor() auth.Descriptor {
	return a.core.Descriptor()
}

// MaxPageSize 返回单页上限。
func (a *readApplication[T, ID]) MaxPageSize() int { return a.core.MaxPageSize() }

// UsesAllowAuthorizer 报告当前应用是否使用一律放行的授权器（D17 启动清单）。
func (a *readApplication[T, ID]) UsesAllowAuthorizer() bool { return a.core.UsesAllowAuthorizer() }

// AuthorizationPlan 返回当前执行入口绑定的声明计划。
func (a *readApplication[T, ID]) AuthorizationPlan() (auth.Plan, bool) {
	return a.core.AuthorizationPlan()
}

// AuthorizationBinding 返回动作在受管理执行骨架中的计划和条件路径。
func (a *ReadApplication[T, ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	return a.core.AuthorizationBinding(action, "app/crud.ReadApplication")
}

// FastDeny 执行声明计划提供的安全提前拒绝。
func (a *readApplication[T, ID]) FastDeny(ctx context.Context, action string) error {
	return a.core.FastDeny(ctx, action)
}
