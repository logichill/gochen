package eventsourced

import (
	"context"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain/eventsourced"
	"gochen/internal/depcheck"
)

// Describe 在 Application 构造前取得事件溯源资源的同源动作描述。
func Describe[T eventsourced.IEventSourcedAggregate[ID], ID comparable](options Options[T, ID]) (auth.Descriptor, error) {
	defaults, err := standardActions(options.Resource, actionPermissions(options.ActionPermissions), !depcheck.IsNil(options.History))
	if err != nil {
		return auth.Descriptor{}, err
	}
	descriptor, _, err := execution.BuildDescriptor(options.Resource, defaults, options.Actions)
	return descriptor, err
}

// AuthorizationPlan 返回当前执行入口绑定的声明计划。
func (a *Application[T, ID]) AuthorizationPlan() (auth.Plan, bool) { return auth.PlanOf(a.authorizer) }

func (a *Application[T, ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	return auth.BindExecution(a.authorizer, a.descriptor, auth.ResourceAccessData, "app/eventsourced.Application", "Application -> aggregate repository(spec, version)", action)
}

// FastDeny 执行声明计划提供的安全提前拒绝。
func (a *Application[T, ID]) FastDeny(ctx context.Context, action string) error {
	return auth.CheckFastDeny(ctx, a.authorizer, action)
}
