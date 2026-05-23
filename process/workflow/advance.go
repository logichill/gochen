package workflow

import (
	"time"

	"gochen/errors"
)

// advanceNode 完成一个活动节点，并根据出边激活后续节点。
//
// 线性流程只有一个后继节点，会直接切换活动节点；分支流程会激活多个后继节点；
// 如果后继节点是汇聚点，则先记录到达分支，直到所有前驱都到达后才真正激活。
func (e *Engine) advanceNode(st *State, def *Definition, nodeID string, now time.Time) *errors.AppError {
	return e.advanceNodeTo(st, def, nodeID, nil, now)
}

// advanceNodeTo 完成一个活动节点，并只激活指定的单个后继节点。
// selectedNextID 为 nil 时沿用默认语义：激活当前节点的全部后继。
// 显式选择只影响从当前节点走哪条出边；目标节点仍遵守原有的 join 等待语义。
//
// 注意：该方法会先校验 selectedNextID，再开始修改状态；调用方若在返回错误后继续复用 st，
// 应自行丢弃这个内存副本并以下一次持久化读取结果为准。
func (e *Engine) advanceNodeTo(st *State, def *Definition, nodeID string, selectedNextID *string, now time.Time) *errors.AppError {
	if !containsString(st.ActiveNodeIDs, nodeID) {
		return errors.NewCode(errors.Conflict, "workflow node is not active").
			WithContext("node_id", nodeID)
	}

	_, node, found := findNode(def, nodeID)
	if !found {
		return errors.NewCode(errors.InvalidInput, "workflow node not found in definition").
			WithContext("node_id", nodeID)
	}
	nextIDs, err := resolveSelectedNext(node.Next, selectedNextID)
	if err != nil {
		return err
	}

	st.ActiveNodeIDs = removeString(st.ActiveNodeIDs, nodeID)
	st.CompletedNodeIDs = append(st.CompletedNodeIDs, nodeID)
	st.History = append(st.History, HistoryEntry{
		Action: "advance",
		NodeID: nodeID,
		At:     now,
	})
	if selectedNextID != nil {
		st.History = append(st.History, HistoryEntry{
			Action:     "choice",
			NodeID:     *selectedNextID,
			FromNodeID: nodeID,
			At:         now,
		})
	}
	if len(nextIDs) > 1 {
		st.History = append(st.History, HistoryEntry{
			Action: "branch",
			NodeID: nodeID,
			At:     now,
		})
	}

	incoming := incomingCounts(def)
	for _, nextNodeID := range nextIDs {
		if err := e.activateNode(st, def, nodeID, nextNodeID, incoming, now); err != nil {
			return err
		}
	}

	refreshCursor(st)
	st.UpdatedAt = now
	if len(st.ActiveNodeIDs) == 0 && len(st.PendingJoins) == 0 {
		st.Status = InstanceStatusCompleted
		st.CompletedAt = now
		st.CurrentNodeID = ""
		st.History = append(st.History, HistoryEntry{
			Action: "complete",
			At:     now,
		})
	}
	return nil
}

func resolveSelectedNext(allNext []string, selected *string) ([]string, *errors.AppError) {
	if selected == nil {
		return allNext, nil
	}
	// 这里按定义中的 Next 做可达性校验；validateDefinition 已保证同一节点的后继不重复。
	target := *selected
	for _, next := range allNext {
		if next == target {
			return []string{target}, nil
		}
	}
	return nil, errors.NewCode(errors.InvalidInput, "workflow selected next node is not reachable").
		WithContext("next_node_id", target)
}

// activateNode 根据目标节点的入边数量决定是直接激活，还是进入汇聚等待。
func (e *Engine) activateNode(st *State, def *Definition, fromNodeID, nodeID string, incoming map[string]int, now time.Time) *errors.AppError {
	if containsString(st.CompletedNodeIDs, nodeID) || containsString(st.ActiveNodeIDs, nodeID) {
		return nil
	}
	if isJoinNode(def, nodeID, incoming) {
		return e.arriveJoin(st, def, nodeID, incoming[nodeID], fromNodeID, now)
	}
	activateReadyNode(st, nodeID)
	return nil
}

func isJoinNode(def *Definition, nodeID string, incoming map[string]int) bool {
	_, node, found := findNode(def, nodeID)
	if !found {
		return incoming[nodeID] > 1
	}
	if node.Kind != "" {
		return node.Kind == NodeKindJoin
	}
	return incoming[nodeID] > 1
}

// arriveJoin 记录一条分支到达汇聚节点；所有前驱到齐后才激活该节点。
func (e *Engine) arriveJoin(st *State, def *Definition, nodeID string, expectedCount int, fromNodeID string, now time.Time) *errors.AppError {
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
	activateReadyNode(st, nodeID)
	st.History = append(st.History, HistoryEntry{
		Action:     "join_ready",
		NodeID:     nodeID,
		FromNodeID: fromNodeID,
		At:         now,
	})
	return nil
}

// activateReadyNode 将节点加入活动集合；已完成或已活动的节点保持幂等。
func activateReadyNode(st *State, nodeID string) {
	if containsString(st.CompletedNodeIDs, nodeID) || containsString(st.ActiveNodeIDs, nodeID) {
		return
	}
	st.ActiveNodeIDs = append(st.ActiveNodeIDs, nodeID)
}

// refreshCursor 在只有一个活动节点时维护兼容读取用的 CurrentNodeID。
func refreshCursor(st *State) {
	if len(st.ActiveNodeIDs) == 1 {
		st.CurrentNodeID = st.ActiveNodeIDs[0]
		return
	}
	st.CurrentNodeID = ""
}
