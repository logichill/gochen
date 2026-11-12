package app

import (
	"context"
	"reflect"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// CommandReader 为聚合、统计等资源专属查询绑定受管理条件。
// 读取函数在组合根装配，业务函数通过 Read 传递查询意图。
type CommandReader[Input, Output any] struct {
	grant *CommandGrant
	read  func(context.Context, Input, spec.Spec) (Output, error)
}

// NewCommandReader 将资源专属读取绑定到声明中的同一仓储实例。
// 读取函数负责在实际仓储调用中执行框架提供的条件；该连接属于装配审核边界。
func NewCommandReader[Input, Output any](repository IExecutionDescriptorProvider, grant *CommandGrant, read func(context.Context, Input, spec.Spec) (Output, error)) (*CommandReader[Input, Output], error) {
	if depcheck.IsNil(repository) || grant == nil || read == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command reader requires a declared repository, grant and scoped reader")
	}
	if !reflect.ValueOf(repository).Comparable() || repository != grant.repository {
		return nil, errors.NewCode(errors.InvalidInput, "reader repository is outside the command resource declaration")
	}
	return &CommandReader[Input, Output]{grant: grant, read: read}, nil
}

// Read 核对执行生命周期与身份绑定，使用资源授权条件及 ID 意图读取独立结果。
func (r *CommandReader[Input, Output]) Read(ctx context.Context, input Input) (Output, error) {
	var zero Output
	if err := r.grant.check(ctx); err != nil {
		return zero, err
	}
	frozen, err := meta.Clone(input)
	if err != nil {
		return zero, err
	}
	output, err := r.read(ctx, frozen, r.grant.readCondition())
	if err != nil {
		return zero, err
	}
	return meta.Clone(output)
}
