package workflow

import (
	"context"
	"strings"
	"time"

	"gochen/errors"
)

// SuspendInstance 将运行中的工作流实例挂起。
func (e *Engine) SuspendInstance(ctx context.Context, instanceID ID, reason string) error {
	return e.SuspendInstanceWithMutation(ctx, instanceID, reason, nil)
}

// SuspendInstanceWithMutation 将运行中的工作流实例挂起，并在持久化前执行补充修改。
func (e *Engine) SuspendInstanceWithMutation(ctx context.Context, instanceID ID, reason string, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(instanceID)) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow instance id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadInstanceAndDefinition(ctx, instanceID)
	if err != nil {
		return err
	}
	if st.Status != InstanceStatusRunning {
		return errors.NewCode(errors.Conflict, "workflow instance is not running").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}

	tctx := &TransitionContext{
		Action:            ActionSuspend,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		st.Status = InstanceStatusSuspended
		st.UpdatedAt = now
		st.History = append(st.History, HistoryEntry{
			Action: "suspend",
			Reason: reason,
			At:     now,
		})
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// ResumeInstance 将被挂起的工作流实例恢复为运行状态。
func (e *Engine) ResumeInstance(ctx context.Context, instanceID ID) error {
	return e.ResumeInstanceWithMutation(ctx, instanceID, nil)
}

// ResumeInstanceWithMutation 将被挂起的工作流实例恢复为运行状态，并在持久化前执行补充修改。
func (e *Engine) ResumeInstanceWithMutation(ctx context.Context, instanceID ID, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(instanceID)) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow instance id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadInstanceAndDefinition(ctx, instanceID)
	if err != nil {
		return err
	}
	if st.Status != InstanceStatusSuspended {
		return errors.NewCode(errors.Conflict, "workflow instance is not suspended").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}

	tctx := &TransitionContext{
		Action:            ActionResume,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		st.Status = InstanceStatusRunning
		st.UpdatedAt = now
		st.History = append(st.History, HistoryEntry{
			Action: "resume",
			At:     now,
		})
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// TerminateInstance 显式终止工作流实例。
func (e *Engine) TerminateInstance(ctx context.Context, instanceID ID, reason string) error {
	return e.TerminateInstanceWithMutation(ctx, instanceID, reason, nil)
}

// TerminateInstanceWithMutation 显式终止工作流实例，并在持久化前执行补充修改。
func (e *Engine) TerminateInstanceWithMutation(ctx context.Context, instanceID ID, reason string, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(instanceID)) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow instance id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadInstanceAndDefinition(ctx, instanceID)
	if err != nil {
		return err
	}
	if st.Status == InstanceStatusCompleted || st.Status == InstanceStatusFailed || st.Status == InstanceStatusTerminated {
		return errors.NewCode(errors.Conflict, "workflow instance is already in terminal status").
			WithContext("instance_id", string(instanceID)).
			WithContext("status", string(st.Status))
	}

	tctx := &TransitionContext{
		Action:            ActionTerminate,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		// 终止会一次性清空活动集合，逐个触发离开钩子，使下游能释放已占用资源。
		for _, activeNodeID := range st.ActiveNodeIDs {
			if err := e.runNodeHooks(c, NodeHookExit, ActionTerminate, st, def, activeNodeID, "", now); err != nil {
				return err
			}
		}
		st.Status = InstanceStatusTerminated
		st.CompletedAt = now
		st.UpdatedAt = now
		st.ActiveNodeIDs = []string{}
		st.ActiveNodeStartedAt = map[string]time.Time{}
		st.PendingJoins = []PendingJoin{}
		st.History = append(st.History, HistoryEntry{
			Action: "terminate",
			Reason: reason,
			At:     now,
		})
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// CheckTimeouts 检查当前实例中的活动节点是否已发生超时。
// 返回已超时的活动节点 ID 列表。
func (e *Engine) CheckTimeouts(ctx context.Context, instanceID ID) ([]string, error) {
	if err := e.validateStore(); err != nil {
		return nil, err
	}
	if err := validateContext(ctx); err != nil {
		return nil, err
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return timedOutNodes(st, def, e.now()), nil
}

// TimedOutInstance 描述一个存在超时活动节点的实例。
type TimedOutInstance struct {
	// InstanceID 是发生超时的实例标识。
	InstanceID ID

	// DefinitionID 是该实例绑定的流程定义标识。
	DefinitionID string

	// NodeIDs 是该实例中已超时的活动节点。
	NodeIDs []string
}

// ScanTimeouts 扫描运行中实例，返回其中存在超时活动节点的实例列表。
//
// 与 CheckTimeouts 的区别：CheckTimeouts 要求调用方已经知道实例 ID，只能做点检；
// ScanTimeouts 面向后台巡检，负责"发现"哪些实例超时。
//
// 该能力要求底层 store 额外实现 IQueryableStore；仅实现 IStore 的存储无法枚举实例，
// 会返回 Unsupported。扫描按最早活动时刻升序取前 limit 条，是一次有界扫描而非全表遍历，
// 因此单次调用不保证返回全部超时实例，应由调用方按固定周期重复执行。
func (e *Engine) ScanTimeouts(ctx context.Context, limit int) ([]TimedOutInstance, error) {
	if err := e.validateStore(); err != nil {
		return nil, err
	}
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, errors.NewCode(errors.InvalidInput, "workflow scan limit must be positive")
	}

	queryable, ok := e.store.(IQueryableStore)
	if !ok {
		return nil, errors.NewCode(errors.Unsupported, "workflow store does not support instance queries")
	}

	states, err := queryable.ListInstances(ctx, InstanceQuery{
		Statuses: []InstanceStatus{InstanceStatusRunning},
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}

	now := e.now()
	var out []TimedOutInstance
	for _, st := range states {
		if st == nil {
			continue
		}
		def, err := e.loadDefinition(ctx, st.DefinitionID, st.DefinitionVersion)
		if err != nil {
			return nil, err
		}
		if def == nil {
			// 定义被删除的实例无法判定超时，跳过而不是让整轮巡检失败。
			continue
		}
		if nodeIDs := timedOutNodes(st, def, now); len(nodeIDs) > 0 {
			out = append(out, TimedOutInstance{
				InstanceID:   st.ID,
				DefinitionID: st.DefinitionID,
				NodeIDs:      nodeIDs,
			})
		}
	}
	return out, nil
}

// timedOutNodes 返回实例中停留时间已达到节点超时时限的活动节点。
func timedOutNodes(st *State, def *Definition, now time.Time) []string {
	var timedOut []string
	for _, nodeID := range st.ActiveNodeIDs {
		_, node, found := findNode(def, nodeID)
		if !found || node.Timeout <= 0 {
			continue
		}
		startedAt, ok := st.ActiveNodeStartedAt[nodeID]
		if !ok || startedAt.IsZero() {
			continue
		}
		if now.Sub(startedAt) >= node.Timeout {
			timedOut = append(timedOut, nodeID)
		}
	}
	return timedOut
}
