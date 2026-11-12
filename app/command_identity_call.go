package app

import (
	"context"
	"fmt"
	"reflect"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// CommandIdentityCall 将全局标识资源的服务调用约束到已授权 ID。
// 适用于资源身份稳定、授权仅引用 ID 的服务端口；有字段或维度条件时使用条件执行仓储。
type CommandIdentityCall[Input any, ID comparable, Output any] struct {
	grant *CommandGrant
	id    func(Input) ID
	call  func(context.Context, Input) (Output, error)
}

// NewCommandIdentityCall 绑定同一资源执行提供方及真实目标标识投影。
// 服务调用函数由组合根装配，必须使用投影对应的目标 ID 执行业务操作。
func NewCommandIdentityCall[Input any, ID comparable, Output any](provider IExecutionDescriptorProvider, grant *CommandGrant, id func(Input) ID, call func(context.Context, Input) (Output, error)) (*CommandIdentityCall[Input, ID, Output], error) {
	if depcheck.IsNil(provider) || grant == nil || id == nil || call == nil {
		return nil, errors.NewCode(errors.InvalidInput, "identity call requires a declared provider, grant, ID projection and call")
	}
	if !reflect.ValueOf(provider).Comparable() || provider != grant.repository {
		return nil, errors.NewCode(errors.InvalidInput, "service provider is outside the command resource declaration")
	}
	if len(grant.decision.Assign) > 0 {
		return nil, errors.NewCode(errors.InvalidInput, "identity call cannot execute resource assignments")
	}
	for _, ref := range grant.decision.Spec.Refs() {
		if ref != meta.ID() {
			return nil, errors.NewCode(errors.InvalidInput, "identity call requires ID-only authorization conditions")
		}
	}
	return &CommandIdentityCall[Input, ID, Output]{grant: grant, id: id, call: call}, nil
}

type commandIdentity[ID comparable] struct {
	ID ID `entity:"id"`
}

// Call 在执行生命周期内核对主体、租户及目标 ID，允许后才调用实际服务。
// 服务失败时仍返回安全复制的部分结果；复制失败时清空结果并合并两类错误。
func (c *CommandIdentityCall[Input, ID, Output]) Call(ctx context.Context, input Input) (Output, error) {
	var zero Output
	if err := c.grant.check(ctx); err != nil {
		return zero, err
	}
	frozen, err := meta.Clone(input)
	if err != nil {
		return zero, err
	}
	projection, err := meta.Clone(frozen)
	if err != nil {
		return zero, err
	}
	id := c.id(projection)
	if len(c.grant.request.IDs) > 0 {
		matched := false
		for _, allowed := range c.grant.request.IDs {
			matched = matched || allowed == fmt.Sprint(id)
		}
		if !matched {
			return zero, errors.NewCode(errors.Forbidden, "service target ID is outside the command intent")
		}
	}
	allowed, err := spec.NewInterpreter().Match(c.grant.decision.Spec, &commandIdentity[ID]{ID: id})
	if err != nil {
		return zero, err
	}
	if !allowed {
		return zero, errors.NewCode(errors.Forbidden, "service target ID is outside the authorized scope")
	}
	output, err := c.call(ctx, frozen)
	cloned, cloneErr := meta.Clone(output)
	if cloneErr != nil {
		return zero, errors.Join(err, cloneErr)
	}
	return cloned, err
}
