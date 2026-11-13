package deletion

import (
	"context"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/errors"
)

// Describe 在 Application 构造前取得删除生命周期的同源动作描述。
func Describe[T IEntity[ID], ID comparable](options Options[T, ID]) (auth.Descriptor, error) {
	if len(options.Actions) != 0 {
		return auth.Descriptor{}, errors.NewCode(errors.InvalidInput, "deletion application does not expose custom actions")
	}
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
	return a.core.AuthorizationBinding(action, "app/crud/deletion.Application")
}

// FastDeny 执行声明计划提供的安全提前拒绝。
func (a *Application[T, ID]) FastDeny(ctx context.Context, action string) error {
	return a.core.FastDeny(ctx, action)
}
