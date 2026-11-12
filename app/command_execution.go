package app

import (
	"context"
	"reflect"
	"slices"

	"gochen/auth"
	"gochen/domain/meta"
	"gochen/errors"
)

// BindExecution 核对嵌套应用与命令声明使用同一仓储、授权器和有效动作计划。
func (g *CommandGrant) BindExecution(repository IExecutionDescriptorProvider, authorizer auth.IAuthorizer, descriptor auth.Descriptor) error {
	if g == nil || g.closed.Load() {
		return errors.NewCode(errors.FailedPrecondition, "command execution is closed")
	}
	compiled, ok := authorizer.(*auth.Authorizer)
	if !ok || compiled != g.authorizer || repository == nil || !reflect.ValueOf(repository).Comparable() || repository != g.repository || descriptor.Resource != g.request.Resource {
		return errors.NewCode(errors.InvalidInput, "application differs from its declared command execution")
	}
	action, ok := descriptor.Action(g.action.Name)
	if !ok || !reflect.DeepEqual(action, g.action) {
		return errors.NewCode(errors.InvalidInput, "application action differs from its command execution")
	}
	permission, ok := descriptor.PermissionFor(action.Name)
	plan, _ := g.authorizer.Plan()
	expected, _ := plan.Descriptor.PermissionFor(action.Name)
	if !ok || permission != expected {
		return errors.NewCode(errors.InvalidInput, "application permission differs from its command execution")
	}
	return nil
}

// ExecutionDecision 复用本次命令的完整判定，持续校验身份、租户、动作、目标及生命周期。
func (g *CommandGrant) ExecutionDecision(ctx context.Context, resource, action string, ids []string) (auth.Decision, error) {
	if ctx == nil {
		return auth.Decision{}, errors.NewCode(errors.InvalidInput, "command context is required")
	}
	if err := g.check(ctx); err != nil {
		return auth.Decision{}, err
	}
	if resource != g.request.Resource || action != g.request.Action || (len(g.request.IDs) > 0 && len(ids) == 0) || (len(g.request.IDs) == 0 && len(ids) > 0) {
		return auth.Decision{}, errors.NewCode(errors.Forbidden, "application execution differs from the authorized command intent")
	}
	for _, id := range ids {
		if !slices.Contains(g.request.IDs, id) {
			return auth.Decision{}, errors.NewCode(errors.Forbidden, "application target is outside the command intent")
		}
	}
	decision := g.decision
	decision.MatchedRules = append([]string(nil), decision.MatchedRules...)
	var err error
	decision.Assign, err = meta.Clone(decision.Assign)
	return decision, err
}
