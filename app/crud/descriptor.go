package crud

import (
	"context"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	"gochen/internal/depcheck"
)

// Describe 在 Application 构造前取得同源动作描述，供组合根编译授权声明。
func Describe[T domain.IEntity[ID], ID comparable](options Options[T, ID], writable bool) (auth.Descriptor, error) {
	defaults, err := standardActions(options.Resource, options.ActionPermissions, writable, !depcheck.IsNil(options.Audit))
	if err != nil {
		return auth.Descriptor{}, err
	}
	descriptor, _, err := execution.BuildDescriptor(options.Resource, defaults, options.Actions)
	return descriptor, err
}

// AuthorizationPlan 返回当前执行入口绑定的声明计划。
func (a *Application[T, ID]) AuthorizationPlan() (auth.Plan, bool) { return a.core.AuthorizationPlan() }

// AuthorizationBinding 返回动作在受管理执行骨架中的计划和条件路径。
func (a *Application[T, ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	return a.core.AuthorizationBinding(action, "app/crud.Application")
}

// FastDeny 执行声明计划提供的安全提前拒绝。
func (a *Application[T, ID]) FastDeny(ctx context.Context, action string) error {
	return a.core.FastDeny(ctx, action)
}
