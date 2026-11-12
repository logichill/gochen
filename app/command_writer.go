package app

import (
	"context"
	"gochen/auth"
	"reflect"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// CommandWriter 连接资源专属原子写入；装配函数在实际仓储中落实授权条件、归属和版本保护。
// 通用实体写入使用 CommandRepository；此端口用于装备切换等具有专属事务语义的操作。
type CommandWriter[Input, Output any] struct {
	grant *CommandGrant
	write func(context.Context, Input, spec.Spec) (Output, error)
}

// NewCommandWriter 要求同一资源实例，专属写入端口拒绝需要权威赋值的声明。
func NewCommandWriter[Input, Output any](repository IExecutionDescriptorProvider, grant *CommandGrant, write func(context.Context, Input, spec.Spec) (Output, error)) (*CommandWriter[Input, Output], error) {
	if depcheck.IsNil(repository) || grant == nil || write == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command writer requires a declared repository, grant and conditional writer")
	}
	if !reflect.ValueOf(repository).Comparable() || repository != grant.repository {
		return nil, errors.NewCode(errors.InvalidInput, "writer repository is outside the command resource declaration")
	}
	if len(grant.decision.Assign) > 0 {
		return nil, errors.NewCode(errors.InvalidInput, "specialized writer cannot execute resource assignments")
	}
	if grant.action.Kind != auth.KindCreate && grant.action.Kind != auth.KindUpdate && grant.action.Kind != auth.KindDelete {
		return nil, errors.NewCode(errors.InvalidInput, "command writer requires a write action")
	}
	return &CommandWriter[Input, Output]{grant: grant, write: write}, nil
}

// Write 核对能力生命周期及身份，以授权条件和冻结 ID 意图执行专属写入。
func (w *CommandWriter[Input, Output]) Write(ctx context.Context, input Input) (Output, error) {
	var zero Output
	if err := w.grant.check(ctx); err != nil {
		return zero, err
	}
	frozen, err := meta.Clone(input)
	if err != nil {
		return zero, err
	}
	output, err := w.write(ctx, frozen, w.grant.readCondition())
	cloned, cloneErr := meta.Clone(output)
	if cloneErr != nil {
		return zero, errors.Join(err, cloneErr)
	}
	return cloned, err
}
