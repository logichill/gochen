package workflow

import (
	"context"
	"time"

	"gochen/errors"
)

// NodeAnyID 用于注册匹配任意节点的钩子。
const NodeAnyID = "*"

// NodeHookPhase 描述节点级钩子的触发时机。
type NodeHookPhase string

const (
	// NodeHookEnter 在节点被激活、加入活动集合时触发。
	// 适用于分派处理人、下发通知、占用额度等"节点开始"语义的副作用。
	NodeHookEnter NodeHookPhase = "enter"

	// NodeHookExit 在节点离开活动集合时触发（推进完成、被驳回或实例被终止）。
	// 适用于释放占用、归档节点产物等"节点结束"语义的收尾动作。
	NodeHookExit NodeHookPhase = "exit"
)

// NodeHookContext 传递一次节点级钩子调用的上下文。
type NodeHookContext struct {
	// Phase 是本次钩子的触发时机。
	Phase NodeHookPhase

	// Action 是触发本次钩子的迁移操作类型，用于区分 advance / reject / terminate 等来源。
	Action ActionType

	// InstanceID 是目标工作流实例标识。
	InstanceID ID

	// DefinitionID 是实例绑定的流程定义标识。
	DefinitionID string

	// DefinitionVersion 是实例绑定的流程定义版本号。
	DefinitionVersion uint32

	// NodeID 是本次钩子对应的节点。
	NodeID string

	// FromNodeID 是驱动本次变化的前驱节点（若适用）。
	FromNodeID string

	// Data 是实例业务数据的**快照**；修改它不会写回实例状态。
	// 需要变更实例数据时使用 *WithMutation 系列入口提供的 StateMutation。
	Data map[string]any

	// At 是本次状态迁移使用的时间戳。
	At time.Time
}

// NodeHook 定义节点级钩子函数签名。
//
// 钩子在实例锁持有期间、状态持久化之前同步执行；返回错误会中止整个状态迁移，
// 实例保持调用前状态。因此钩子应只做轻量校验或可重试的副作用，不要包含长耗时 IO。
//
// 交付语义是 at-least-once：钩子成功返回后，同一次迁移仍可能因为后续钩子失败或
// 持久化版本冲突而整体回滚，此时已执行的副作用不会被撤销。带外部副作用的钩子
// 必须自身幂等，或把副作用改为写入实例数据、由调用方在提交后另行触发。
type NodeHook func(ctx context.Context, hctx *NodeHookContext) error

// nodeHookRegistry 按 phase 与节点 ID 保存已注册的节点级钩子。
type nodeHookRegistry map[NodeHookPhase]map[string][]NodeHook

func (r nodeHookRegistry) register(phase NodeHookPhase, nodeID string, hooks ...NodeHook) {
	if len(hooks) == 0 {
		return
	}
	byNode := r[phase]
	if byNode == nil {
		byNode = map[string][]NodeHook{}
		r[phase] = byNode
	}
	for _, hook := range hooks {
		if hook != nil {
			byNode[nodeID] = append(byNode[nodeID], hook)
		}
	}
}

// lookup 返回某节点在指定 phase 上应执行的钩子：先通配钩子，再节点专属钩子。
func (r nodeHookRegistry) lookup(phase NodeHookPhase, nodeID string) []NodeHook {
	byNode := r[phase]
	if len(byNode) == 0 {
		return nil
	}
	wildcard := byNode[NodeAnyID]
	specific := byNode[nodeID]
	if len(wildcard) == 0 {
		return specific
	}
	if len(specific) == 0 {
		return wildcard
	}
	out := make([]NodeHook, 0, len(wildcard)+len(specific))
	out = append(out, wildcard...)
	out = append(out, specific...)
	return out
}

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
