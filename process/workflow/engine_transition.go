package workflow

import (
	"context"
	"strings"

	"gochen/errors"
)

// Advance 完成当前唯一活动节点并推进流程。
func (e *Engine) Advance(ctx context.Context, instanceID ID) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if len(st.ActiveNodeIDs) != 1 {
		return errors.NewCode(errors.Conflict, "workflow instance requires explicit node selection").
			WithContext("instance_id", string(instanceID)).
			WithContext("active_nodes", len(st.ActiveNodeIDs))
	}

	nodeID := st.ActiveNodeIDs[0]
	tctx := &TransitionContext{
		Action:            ActionAdvance,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		if err := e.advanceNode(c, st, def, nodeID, e.now()); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// AdvanceNode 完成指定活动节点并继续推进工作流。
func (e *Engine) AdvanceNode(ctx context.Context, instanceID ID, nodeID string) error {
	return e.AdvanceNodeWithMutation(ctx, instanceID, nodeID, nil)
}

// AdvanceNodeWithMutation 完成指定活动节点，并在同一次持久化前执行调用方提供的状态补充逻辑。
func (e *Engine) AdvanceNodeWithMutation(ctx context.Context, instanceID ID, nodeID string, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow node id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	tctx := &TransitionContext{
		Action:            ActionAdvance,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		if err := e.advanceNode(c, st, def, nodeID, now); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// AdvanceNodeTo 完成指定活动节点，并只激活给定的单个后继节点。
func (e *Engine) AdvanceNodeTo(ctx context.Context, instanceID ID, nodeID string, nextNodeID string) error {
	return e.AdvanceNodeToWithMutation(ctx, instanceID, nodeID, nextNodeID, nil)
}

// AdvanceNodeToWithMutation 完成指定活动节点，显式选择后继，并在同一次持久化前执行状态补充逻辑。
func (e *Engine) AdvanceNodeToWithMutation(ctx context.Context, instanceID ID, nodeID string, nextNodeID string, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow node id is empty")
	}
	if strings.TrimSpace(nextNodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow next node id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	tctx := &TransitionContext{
		Action:            ActionAdvance,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		TargetNodeID:      nextNodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		if err := e.advanceNodeTo(c, st, def, nodeID, &nextNodeID, now); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// RejectNode 将指定活动节点驳回到定义允许的目标节点。
func (e *Engine) RejectNode(ctx context.Context, instanceID ID, nodeID string, targetNodeID string) error {
	return e.RejectNodeWithMutation(ctx, instanceID, nodeID, targetNodeID, nil)
}

// RejectNodeWithMutation 将指定活动节点驳回，并在同一次持久化前执行状态补充逻辑。
func (e *Engine) RejectNodeWithMutation(ctx context.Context, instanceID ID, nodeID string, targetNodeID string, mutate StateMutation) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow node id is empty")
	}
	if strings.TrimSpace(targetNodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow reject target node id is empty")
	}

	unlock := e.lockInstance(instanceID)
	defer unlock()

	st, def, err := e.loadRunningInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	tctx := &TransitionContext{
		Action:            ActionReject,
		InstanceID:        instanceID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		NodeID:            nodeID,
		TargetNodeID:      targetNodeID,
		Data:              cloneWorkflowMap(st.Data),
	}

	return executeWithInterceptors(ctx, e.interceptors, tctx, func(c context.Context) error {
		now := e.now()
		if err := e.rejectNode(c, st, def, nodeID, targetNodeID, now); err != nil {
			return err.WithContext("instance_id", string(instanceID))
		}
		if mutate != nil {
			if err := mutate(st, now); err != nil {
				return err
			}
		}
		return e.saveInstance(c, st, st.Version)
	})
}

// updateInstanceData 对已存在实例的 Data 字段执行受限更新。
func (e *Engine) updateInstanceData(ctx context.Context, instance ID, update func(data map[string]any) error) error {
	if err := e.validateStore(); err != nil {
		return err
	}
	if err := validateContext(ctx); err != nil {
		return err
	}
	if update == nil {
		return errors.NewCode(errors.InvalidInput, "update function cannot be nil")
	}

	unlock := e.lockInstance(instance)
	defer unlock()

	st, err := e.loadInstance(ctx, instance)
	if err != nil {
		return err
	}
	if st == nil {
		return errors.NewCode(errors.NotFound, "workflow instance not found").
			WithContext("instance_id", string(instance))
	}
	if st.Data == nil {
		st.Data = map[string]any{}
	}
	if err := update(st.Data); err != nil {
		return err
	}
	st.UpdatedAt = e.now()
	return e.saveInstance(ctx, st, st.Version)
}
