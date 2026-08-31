package workflow

import (
	"context"
	"time"

	"gochen/errors"
)

// rejectNode 将一个活动节点驳回到定义允许的支配祖先节点。
//
// 驳回会清理目标节点之后由该目标支配的已完成、活动与汇聚等待状态，确保目标节点重新提交时
// 不会被旧的 CompletedNodeIDs 或 PendingJoins 跳过。
func (e *Engine) rejectNode(ctx context.Context, st *State, def *Definition, nodeID string, targetNodeID string, now time.Time) errors.IAppError {
	if !containsString(st.ActiveNodeIDs, nodeID) {
		return errors.NewCode(errors.Conflict, "workflow node is not active").
			WithContext("node_id", nodeID)
	}
	_, node, found := findNode(def, nodeID)
	if !found {
		return errors.NewCode(errors.InvalidInput, "workflow node not found in definition").
			WithContext("node_id", nodeID)
	}
	if !containsString(node.RejectTo, targetNodeID) {
		return errors.NewCode(errors.InvalidInput, "workflow reject target is not allowed").
			WithContext("node_id", nodeID).
			WithContext("reject_to", targetNodeID)
	}
	nodes := nodeMap(def)
	idx := buildDominanceIndex(def.StartNodeID, nodes)
	if !strictDominatesNodeWithIndex(def.StartNodeID, targetNodeID, nodeID, idx, nodes) {
		return errors.NewCode(errors.InvalidInput, "workflow reject target must dominate node").
			WithContext("node_id", nodeID).
			WithContext("reject_to", targetNodeID)
	}

	// 被驳回节点先离开活动集合，钩子可在此释放该节点占用的资源。
	if err := e.runNodeHooks(ctx, NodeHookExit, ActionReject, st, def, nodeID, targetNodeID, now); err != nil {
		return err
	}

	var resetNodes map[string]struct{}
	if idx != nil {
		resetNodes = dominatedReachableNodesWithIndex(targetNodeID, idx)
	} else {
		resetNodes = dominatedReachableNodes(def, targetNodeID)
	}
	st.ActiveNodeIDs = removeNodes(st.ActiveNodeIDs, resetNodes)
	if st.ActiveNodeStartedAt != nil {
		for resetNode := range resetNodes {
			delete(st.ActiveNodeStartedAt, resetNode)
		}
	}
	st.CompletedNodeIDs = removeNodes(st.CompletedNodeIDs, resetNodes)
	st.PendingJoins = removeResetPendingJoins(st.PendingJoins, resetNodes)
	activated := activateReadyNode(st, targetNodeID, now)
	st.Status = InstanceStatusRunning
	st.CompletedAt = time.Time{}
	st.UpdatedAt = now
	st.History = append(st.History, HistoryEntry{
		Action:     "reject",
		NodeID:     targetNodeID,
		FromNodeID: nodeID,
		At:         now,
	})
	if activated {
		return e.runNodeHooks(ctx, NodeHookEnter, ActionReject, st, def, targetNodeID, nodeID, now)
	}
	return nil
}

func removeNodes(values []string, removed map[string]struct{}) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := removed[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func removeResetPendingJoins(joins []PendingJoin, resetNodes map[string]struct{}) []PendingJoin {
	if joins == nil {
		return nil
	}
	out := make([]PendingJoin, 0, len(joins))
	for _, join := range joins {
		if _, resetJoin := resetNodes[join.NodeID]; resetJoin {
			continue
		}
		join.ArrivedFrom = removeNodes(join.ArrivedFrom, resetNodes)
		if len(join.ArrivedFrom) == 0 {
			continue
		}
		out = append(out, join)
	}
	return out
}
