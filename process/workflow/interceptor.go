package workflow

import (
	"context"
)

// ActionType 描述流程状态迁移的操作类型。
type ActionType string

const (
	ActionCreate    ActionType = "create"
	ActionStart     ActionType = "start"
	ActionAdvance   ActionType = "advance"
	ActionReject    ActionType = "reject"
	ActionSuspend   ActionType = "suspend"
	ActionResume    ActionType = "resume"
	ActionTerminate ActionType = "terminate"
)

// TransitionContext 传递一次状态迁移过程中的上下文信息。
type TransitionContext struct {
	// Action 是本次迁移操作的类型。
	Action ActionType

	// InstanceID 是目标工作流实例的唯一标识。
	InstanceID ID

	// DefinitionID 是实例绑定的流程定义标识。
	DefinitionID string

	// DefinitionVersion 是实例绑定的流程定义版本号。
	DefinitionVersion uint32

	// NodeID 是本次操作触发的活动节点（若适用）。
	NodeID string

	// TargetNodeID 是本次操作目标节点（例如驳回或显式选择的分支）。
	TargetNodeID string

	// Data 是实例当前挂载的业务数据。
	Data map[string]any
}

// Interceptor 定义工作流状态迁移拦截器函数签名。
// 类似于 HTTP 中间件的洋葱圈模型，可用于链路追踪、审计、权限校验与指标统计。
type Interceptor func(ctx context.Context, tctx *TransitionContext, next func(ctx context.Context) error) error

// executeWithInterceptors 按注册顺序执行拦截器链。
func executeWithInterceptors(
	ctx context.Context,
	interceptors []Interceptor,
	tctx *TransitionContext,
	handler func(ctx context.Context) error,
) error {
	if len(interceptors) == 0 {
		return handler(ctx)
	}

	var buildChain func(int) func(context.Context) error
	buildChain = func(index int) func(context.Context) error {
		if index == len(interceptors) {
			return handler
		}
		return func(c context.Context) error {
			return interceptors[index](c, tctx, buildChain(index+1))
		}
	}

	return buildChain(0)(ctx)
}
