package crud

import (
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	"gochen/errors"
)

// Describe 在 Application 构造前取得同源动作描述，供组合根编译授权声明。
func Describe[T domain.IEntity[ID], ID comparable](options Options[T, ID], writable bool) (auth.Descriptor, error) {
	for _, action := range options.Actions {
		if action.Name == execution.ActionAuditRead {
			return auth.Descriptor{}, errors.NewCode(errors.InvalidInput, "audit_read belongs to the independent audit reader")
		}
	}
	defaults, err := standardActions(options.Resource, options.ActionPermissions, writable, canDelete[T, ID]())
	if err != nil {
		return auth.Descriptor{}, err
	}
	descriptor, _, err := execution.BuildDescriptor(options.Resource, defaults, options.Actions)
	return descriptor, err
}

// AuthorizationBinding 返回动作在受管理执行骨架中的计划和条件路径。
func (a *Application[T, ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	return a.core.AuthorizationBinding(action, "app/crud.Application")
}
