package workflow

import (
	"context"
	"time"

	"gochen/errors"
)

// advanceNode 完成一个活动节点，并根据出边（及条件求值）激活后续节点。
//
// 线性流程只有一个后继节点，会直接切换活动节点；分支流程会激活满足条件的一个或多个后继节点；
// 如果后继节点是汇聚点，则先记录到达分支，直到所有前驱都到达后才真正激活。
func (e *Engine) advanceNode(ctx context.Context, st *State, def *Definition, nodeID string, now time.Time) errors.IAppError {
	return e.advanceNodeTo(ctx, st, def, nodeID, nil, now)
}

// advanceNodeTo 完成一个活动节点，并只激活指定的单个后继节点（或自动评估条件出边）。
// selectedNextID 为 nil 时根据出边条件表达式自动选择满足条件的分支。
//
// 状态迁移会先作用在副本上，全部成功后再写回 st；返回错误时 st 保持调用前状态。
func (e *Engine) advanceNodeTo(ctx context.Context, st *State, def *Definition, nodeID string, selectedNextID *string, now time.Time) errors.IAppError {
	if !containsString(st.ActiveNodeIDs, nodeID) {
		return errors.NewCode(errors.Conflict, "workflow node is not active").
			WithContext("node_id", nodeID)
	}

	_, node, found := findNode(def, nodeID)
	if !found {
		return errors.NewCode(errors.InvalidInput, "workflow node not found in definition").
			WithContext("node_id", nodeID)
	}

	nextIDs, err := e.resolveNextNodes(ctx, node.Edges, st.Data, selectedNextID)
	if err != nil {
		return err
	}

	// 离开钩子放在后继解析之后：解析本身是纯计算，先算完再触发副作用，
	// 可以避免"无出边命中"这类必然失败的推进白白执行一遍钩子。
	if err := e.runNodeHooks(ctx, NodeHookExit, ActionAdvance, st, def, nodeID, "", now); err != nil {
		return err
	}

	next := cloneState(st)
	next.ActiveNodeIDs = removeString(next.ActiveNodeIDs, nodeID)
	if next.ActiveNodeStartedAt != nil {
		delete(next.ActiveNodeStartedAt, nodeID)
	}
	next.CompletedNodeIDs = append(next.CompletedNodeIDs, nodeID)
	next.History = append(next.History, HistoryEntry{
		Action: "advance",
		NodeID: nodeID,
		At:     now,
	})
	if selectedNextID != nil {
		next.History = append(next.History, HistoryEntry{
			Action:     "choice",
			NodeID:     *selectedNextID,
			FromNodeID: nodeID,
			At:         now,
		})
	}
	if len(nextIDs) > 1 {
		next.History = append(next.History, HistoryEntry{
			Action: "branch",
			NodeID: nodeID,
			At:     now,
		})
	}

	incoming := incomingCounts(def)
	for _, nextNodeID := range nextIDs {
		if err := e.activateNode(ctx, next, def, nodeID, nextNodeID, incoming, now); err != nil {
			return err
		}
	}

	next.UpdatedAt = now
	if len(next.ActiveNodeIDs) == 0 && len(next.PendingJoins) == 0 {
		next.Status = InstanceStatusCompleted
		next.CompletedAt = now
		next.History = append(next.History, HistoryEntry{
			Action: "complete",
			At:     now,
		})
	}
	*st = *next
	return nil
}

func (e *Engine) resolveNextNodes(ctx context.Context, edges []Edge, data map[string]any, selected *string) ([]string, errors.IAppError) {
	if selected != nil {
		target := *selected
		for _, edge := range edges {
			if edge.Target == target {
				return []string{target}, nil
			}
		}
		return nil, errors.NewCode(errors.InvalidInput, "workflow selected next node is not reachable").
			WithContext("next_node_id", target)
	}

	// 终结节点：没有出边，实例在此收敛。
	if len(edges) == 0 {
		return nil, nil
	}

	evaluator := e.evaluator
	if evaluator == nil {
		evaluator = NewDefaultConditionEvaluator()
	}

	var matched []string
	var defaults []string
	for _, edge := range edges {
		if edge.Default {
			defaults = append(defaults, edge.Target)
			continue
		}
		if edge.Condition == "" {
			matched = append(matched, edge.Target)
			continue
		}
		hit, err := evaluator.Evaluate(ctx, edge.Condition, data)
		if err != nil {
			return nil, errors.NewCodeWithCause(errors.Internal, "failed to evaluate edge condition", err).
				WithContext("target", edge.Target).
				WithContext("condition", edge.Condition)
		}
		if hit {
			matched = append(matched, edge.Target)
		}
	}
	if len(matched) > 0 {
		return matched, nil
	}
	if len(defaults) > 0 {
		return defaults, nil
	}

	// 没有任何出边命中：必须显式报错。若静默返回空集合，调用方会看到实例被判定为
	// completed，而流程实际上从未到达任何终结节点。
	return nil, errors.NewCode(errors.Conflict, "workflow no outgoing edge matched").
		WithContext("edges", len(edges))
}

// activateNode 根据目标节点的入边数量决定是直接激活，还是进入汇聚等待。
func (e *Engine) activateNode(ctx context.Context, st *State, def *Definition, fromNodeID, nodeID string, incoming map[string]int, now time.Time) errors.IAppError {
	if containsString(st.CompletedNodeIDs, nodeID) || containsString(st.ActiveNodeIDs, nodeID) {
		return nil
	}
	if isJoinNode(def, nodeID) {
		return e.arriveJoin(ctx, st, def, nodeID, incoming[nodeID], fromNodeID, now)
	}
	if activateReadyNode(st, nodeID, now) {
		return e.runNodeHooks(ctx, NodeHookEnter, ActionAdvance, st, def, nodeID, fromNodeID, now)
	}
	return nil
}

func isJoinNode(def *Definition, nodeID string) bool {
	_, node, found := findNode(def, nodeID)
	if !found {
		return false
	}
	return node.Kind == NodeKindJoin
}

// arriveJoin 记录一条分支到达汇聚节点；所有前驱到齐后才激活该节点。
func (e *Engine) arriveJoin(ctx context.Context, st *State, def *Definition, nodeID string, expectedCount int, fromNodeID string, now time.Time) errors.IAppError {
	if _, _, found := findNode(def, nodeID); !found {
		return errors.NewCode(errors.InvalidInput, "workflow node not found in definition").
			WithContext("node_id", nodeID)
	}
	join := findPendingJoin(st.PendingJoins, nodeID)
	if join == nil {
		st.PendingJoins = append(st.PendingJoins, PendingJoin{
			NodeID:        nodeID,
			ExpectedCount: expectedCount,
			ArrivedFrom:   []string{},
		})
		join = &st.PendingJoins[len(st.PendingJoins)-1]
	}
	if containsString(join.ArrivedFrom, fromNodeID) {
		return errors.NewCode(errors.Conflict, "workflow join branch already arrived").
			WithContext("node_id", nodeID).
			WithContext("from_node_id", fromNodeID)
	}

	join.ArrivedFrom = append(join.ArrivedFrom, fromNodeID)
	if len(join.ArrivedFrom) < join.ExpectedCount {
		st.History = append(st.History, HistoryEntry{
			Action:     "join_wait",
			NodeID:     nodeID,
			FromNodeID: fromNodeID,
			At:         now,
		})
		return nil
	}

	st.PendingJoins = removePendingJoin(st.PendingJoins, nodeID)
	activated := activateReadyNode(st, nodeID, now)
	st.History = append(st.History, HistoryEntry{
		Action:     "join_ready",
		NodeID:     nodeID,
		FromNodeID: fromNodeID,
		At:         now,
	})
	if activated {
		return e.runNodeHooks(ctx, NodeHookEnter, ActionAdvance, st, def, nodeID, fromNodeID, now)
	}
	return nil
}

// activateReadyNode 将节点加入活动集合并记录激活时间；已完成或已活动的节点保持幂等。
// 返回值表示本次调用是否真的激活了该节点，供节点进入钩子判断是否触发。
func activateReadyNode(st *State, nodeID string, now time.Time) bool {
	if containsString(st.CompletedNodeIDs, nodeID) || containsString(st.ActiveNodeIDs, nodeID) {
		return false
	}
	st.ActiveNodeIDs = append(st.ActiveNodeIDs, nodeID)
	if st.ActiveNodeStartedAt == nil {
		st.ActiveNodeStartedAt = map[string]time.Time{}
	}
	st.ActiveNodeStartedAt[nodeID] = now
	return true
}
