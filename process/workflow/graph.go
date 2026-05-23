package workflow

import "gochen/errors"

// walkNodeGraph 从起点深度遍历流程图，用递归栈识别环路。
func walkNodeGraph(nodeID string, nodes map[string]Node, visited map[string]int, stack map[string]bool) *errors.AppError {
	if stack[nodeID] {
		return errors.NewCode(errors.InvalidInput, "workflow graph must be acyclic").
			WithContext("node_id", nodeID)
	}
	if visited[nodeID] > 0 {
		return nil
	}

	stack[nodeID] = true
	visited[nodeID] = 1
	node := nodes[nodeID]
	for _, nextNodeID := range node.Next {
		if err := walkNodeGraph(nextNodeID, nodes, visited, stack); err != nil {
			return err
		}
	}
	delete(stack, nodeID)
	return nil
}

// findNode 在定义中按 ID 查找节点，并返回索引和值。
func findNode(def *Definition, nodeID string) (int, Node, bool) {
	for i, node := range def.Nodes {
		if node.ID == nodeID {
			return i, node, true
		}
	}
	return -1, Node{}, false
}

// incomingCounts 统计每个节点的入边数量，用于判断汇聚节点。
func incomingCounts(def *Definition) map[string]int {
	counts := make(map[string]int, len(def.Nodes))
	for _, node := range def.Nodes {
		for _, nextNodeID := range node.Next {
			counts[nextNodeID]++
		}
	}
	return counts
}

// containsString 判断字符串切片中是否包含目标值。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// removeString 返回移除目标值后的新切片，不修改原切片。
func removeString(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

// findPendingJoin 查找指定节点当前等待中的汇聚状态。
func findPendingJoin(joins []PendingJoin, nodeID string) *PendingJoin {
	for i := range joins {
		if joins[i].NodeID == nodeID {
			return &joins[i]
		}
	}
	return nil
}

// removePendingJoin 返回移除指定汇聚状态后的新切片。
func removePendingJoin(joins []PendingJoin, nodeID string) []PendingJoin {
	out := make([]PendingJoin, 0, len(joins))
	for _, join := range joins {
		if join.NodeID != nodeID {
			out = append(out, join)
		}
	}
	return out
}
