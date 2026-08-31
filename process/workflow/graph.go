package workflow

import "gochen/errors"

type dominanceIndex struct {
	dominators map[string]map[string]struct{}
	dominated  map[string]map[string]struct{}
}

// walkNodeGraph 从起点深度遍历流程图，用递归栈识别环路。
func walkNodeGraph(nodeID string, nodes map[string]Node, visited map[string]int, stack map[string]bool) errors.IAppError {
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
	for _, edge := range node.Edges {
		if err := walkNodeGraph(edge.Target, nodes, visited, stack); err != nil {
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

func nodeMap(def *Definition) map[string]Node {
	if def == nil {
		return nil
	}
	nodes := make(map[string]Node, len(def.Nodes))
	for _, node := range def.Nodes {
		nodes[node.ID] = node
	}
	return nodes
}

// incomingCounts 统计每个节点的入边数量，用于判断汇聚节点。
func incomingCounts(def *Definition) map[string]int {
	counts := make(map[string]int, len(def.Nodes))
	for _, node := range def.Nodes {
		for _, edge := range node.Edges {
			counts[edge.Target]++
		}
	}
	return counts
}

// strictDominates 判断 dominatorID 是否严格支配 nodeID，并负责从完整定义构建节点表与索引。
func strictDominates(def *Definition, dominatorID string, nodeID string) bool {
	if def == nil {
		return false
	}
	nodes := nodeMap(def)
	return strictDominatesNodeWithIndex(def.StartNodeID, dominatorID, nodeID, buildDominanceIndex(def.StartNodeID, nodes), nodes)
}

// strictDominatesNode 在调用方已持有节点表时判断严格支配关系。
func strictDominatesNode(startNodeID string, dominatorID string, nodeID string, nodes map[string]Node) bool {
	return strictDominatesNodeWithIndex(startNodeID, dominatorID, nodeID, buildDominanceIndex(startNodeID, nodes), nodes)
}

// strictDominatesNodeWithIndex 复用 dominanceIndex；idx 为 nil 时回退到可达性判断。
func strictDominatesNodeWithIndex(startNodeID string, dominatorID string, nodeID string, idx *dominanceIndex, nodes map[string]Node) bool {
	if dominatorID == "" || nodeID == "" || dominatorID == nodeID {
		return false
	}
	if _, ok := nodes[dominatorID]; !ok {
		return false
	}
	if _, ok := nodes[nodeID]; !ok {
		return false
	}
	if idx != nil {
		if doms := idx.dominators[nodeID]; doms != nil {
			_, ok := doms[dominatorID]
			return ok
		}
		return false
	}
	return !canReachAvoiding(startNodeID, nodeID, dominatorID, nodes)
}

// canReachAvoiding 判断从 startNodeID 到 targetNodeID 是否存在一条不经过 blockedNodeID 的路径。
func canReachAvoiding(startNodeID string, targetNodeID string, blockedNodeID string, nodes map[string]Node) bool {
	if startNodeID == "" || targetNodeID == "" {
		return false
	}
	if startNodeID == blockedNodeID {
		return false
	}
	visited := make(map[string]struct{}, len(nodes))
	stack := []string{startNodeID}
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if nodeID == blockedNodeID {
			continue
		}
		if nodeID == targetNodeID {
			return true
		}
		if _, seen := visited[nodeID]; seen {
			continue
		}
		visited[nodeID] = struct{}{}
		node, ok := nodes[nodeID]
		if !ok {
			continue
		}
		for _, edge := range node.Edges {
			stack = append(stack, edge.Target)
		}
	}
	return false
}

// dominatedReachableNodes 返回 dominatorID 自身及其严格支配的可达节点集合。
func dominatedReachableNodes(def *Definition, dominatorID string) map[string]struct{} {
	out := map[string]struct{}{}
	if def == nil || dominatorID == "" {
		return out
	}
	nodes := nodeMap(def)
	if _, ok := nodes[dominatorID]; !ok {
		return out
	}
	if idx := buildDominanceIndex(def.StartNodeID, nodes); idx != nil {
		return dominatedReachableNodesWithIndex(dominatorID, idx)
	}
	visited := map[string]struct{}{}
	stack := []string{dominatorID}
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := visited[nodeID]; seen {
			continue
		}
		visited[nodeID] = struct{}{}
		if nodeID == dominatorID || strictDominatesNode(def.StartNodeID, dominatorID, nodeID, nodes) {
			out[nodeID] = struct{}{}
		}
		node, ok := nodes[nodeID]
		if !ok {
			continue
		}
		for _, edge := range node.Edges {
			stack = append(stack, edge.Target)
		}
	}
	return out
}

// dominatedReachableNodesWithIndex 通过 dominanceIndex 的反向索引读取被支配节点集合。
func dominatedReachableNodesWithIndex(dominatorID string, idx *dominanceIndex) map[string]struct{} {
	out := map[string]struct{}{}
	if dominatorID == "" || idx == nil {
		return out
	}
	out[dominatorID] = struct{}{}
	for nodeID := range idx.dominated[dominatorID] {
		out[nodeID] = struct{}{}
	}
	return out
}

// buildDominanceIndex 为 DAG 计算支配者与被支配者索引；非 DAG 时返回 nil 让调用方回退。
func buildDominanceIndex(startNodeID string, nodes map[string]Node) *dominanceIndex {
	if startNodeID == "" || len(nodes) == 0 {
		return nil
	}
	if _, ok := nodes[startNodeID]; !ok {
		return nil
	}

	// 先构造前驱表，并用 Kahn 拓扑排序确认图是 DAG；发现环时退回到旧的逐次可达性判断。
	predecessors := make(map[string][]string, len(nodes))
	inDegree := make(map[string]int, len(nodes))
	for nodeID := range nodes {
		inDegree[nodeID] = 0
	}
	for nodeID, node := range nodes {
		for _, edge := range node.Edges {
			if _, ok := nodes[edge.Target]; !ok {
				continue
			}
			predecessors[edge.Target] = append(predecessors[edge.Target], nodeID)
			inDegree[edge.Target]++
		}
	}

	queue := make([]string, 0, len(nodes))
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}
	order := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		order = append(order, nodeID)
		for _, edge := range nodes[nodeID].Edges {
			if _, ok := inDegree[edge.Target]; !ok {
				continue
			}
			inDegree[edge.Target]--
			if inDegree[edge.Target] == 0 {
				queue = append(queue, edge.Target)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil
	}

	// DAG 上按拓扑序计算支配集合：某节点的支配者是所有前驱支配集合的交集，再加节点自身。
	dominators := make(map[string]map[string]struct{}, len(nodes))
	for _, nodeID := range order {
		if nodeID == startNodeID {
			dominators[nodeID] = map[string]struct{}{startNodeID: {}}
			continue
		}
		preds := predecessors[nodeID]
		if len(preds) == 0 {
			dominators[nodeID] = map[string]struct{}{nodeID: {}}
			continue
		}
		intersection := cloneStringSet(dominators[preds[0]])
		for _, predID := range preds[1:] {
			intersection = intersectStringSets(intersection, dominators[predID])
		}
		intersection[nodeID] = struct{}{}
		delete(intersection, "")
		dominators[nodeID] = intersection
	}

	// 反向索引便于从“支配者”快速找到所有被它严格支配的节点。
	dominated := make(map[string]map[string]struct{}, len(nodes))
	for nodeID, doms := range dominators {
		for domID := range doms {
			if domID == nodeID {
				continue
			}
			if dominated[domID] == nil {
				dominated[domID] = make(map[string]struct{})
			}
			dominated[domID][nodeID] = struct{}{}
		}
	}

	return &dominanceIndex{
		dominators: dominators,
		dominated:  dominated,
	}
}

func cloneStringSet(values map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(values))
	for value := range values {
		cloned[value] = struct{}{}
	}
	return cloned
}

func intersectStringSets(left map[string]struct{}, right map[string]struct{}) map[string]struct{} {
	if len(left) == 0 || len(right) == 0 {
		return map[string]struct{}{}
	}
	if len(left) > len(right) {
		left, right = right, left
	}
	out := make(map[string]struct{}, len(left))
	for value := range left {
		if _, ok := right[value]; ok {
			out[value] = struct{}{}
		}
	}
	return out
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
