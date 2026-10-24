package workflow

import (
	"context"
	"time"

	"gochen/errors"
)

// OnNodeEnter 注册节点进入钩子；nodeID 传 NodeAnyID 表示匹配所有节点。
//
// 必须在引擎开始处理实例前完成注册；注册本身不是并发安全的。
func (e *Engine) OnNodeEnter(nodeID string, hooks ...NodeHook) *Engine {
	return e.onNode(NodeHookEnter, nodeID, hooks...)
}

// OnNodeExit 注册节点离开钩子；nodeID 传 NodeAnyID 表示匹配所有节点。
//
// 必须在引擎开始处理实例前完成注册；注册本身不是并发安全的。
func (e *Engine) OnNodeExit(nodeID string, hooks ...NodeHook) *Engine {
	return e.onNode(NodeHookExit, nodeID, hooks...)
}

func (e *Engine) onNode(phase NodeHookPhase, nodeID string, hooks ...NodeHook) *Engine {
	if e == nil {
		return e
	}
	if nodeID == "" {
		return e
	}
	if e.nodeHooks == nil {
		e.nodeHooks = nodeHookRegistry{}
	}
	e.nodeHooks.register(phase, nodeID, hooks...)
	return e
}

// runNodeHooks 执行某个节点在指定 phase 上的全部钩子。
// 任一钩子返回错误即中止，错误会被归一化为 IAppError 并带上定位上下文。
func (e *Engine) runNodeHooks(
	ctx context.Context,
	phase NodeHookPhase,
	action ActionType,
	st *State,
	def *Definition,
	nodeID string,
	fromNodeID string,
	now time.Time,
) errors.IAppError {
	if e == nil || len(e.nodeHooks) == 0 {
		return nil
	}
	hooks := e.nodeHooks.lookup(phase, nodeID)
	if len(hooks) == 0 {
		return nil
	}

	hctx := &NodeHookContext{
		Phase:             phase,
		Action:            action,
		InstanceID:        st.ID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		FromNodeID:        fromNodeID,
		Data:              cloneWorkflowMap(st.Data),
		At:                now,
	}
	for _, hook := range hooks {
		if err := hook(ctx, hctx); err != nil {
			return errors.Wrap(err, errors.Internal, "workflow node hook failed").
				WithContext("phase", string(phase)).
				WithContext("action", string(action)).
				WithContext("instance_id", string(st.ID)).
				WithContext("node_id", nodeID)
		}
	}
	return nil
}
