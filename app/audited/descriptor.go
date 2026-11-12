package audited

import (
	"context"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain/audited"
)

// Describe 在 Application 构造前取得审计型资源的同源动作描述。
func Describe[T audited.IAuditedEntity[ID], ID comparable](options Options[T, ID]) (auth.Descriptor, error) {
	defaults, err := standardActions(options.Resource, options.ActionPermissions)
	if err != nil {
		return auth.Descriptor{}, err
	}
	descriptor, _, err := execution.BuildDescriptor(options.Resource, defaults, options.Actions)
	return descriptor, err
}

// AuthorizationPlan 返回当前执行入口绑定的声明计划。
func (a *Application[T, ID]) AuthorizationPlan() (auth.Plan, bool) { return a.core.AuthorizationPlan() }

func (a *Application[T, ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	return a.core.AuthorizationBinding(action, "app/audited.Application")
}

// FastDeny 执行声明计划提供的安全提前拒绝。
func (a *Application[T, ID]) FastDeny(ctx context.Context, action string) error {
	return a.core.FastDeny(ctx, action)
}
