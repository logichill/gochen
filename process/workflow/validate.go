package workflow

import (
	"strings"

	"gochen/errors"
)

// ValidateDefinition 校验流程定义是从起点可达的无环 DAG。
//
// 校验项包括：定义与节点 ID 非空、节点 ID 唯一、StartNodeID 存在、出边指向合法、
// 同一节点的后继目标不重复、默认边最多一条且不带条件、条件分支必须有兜底出边、
// 节点超时时限非负、从起点遍历无环且能覆盖全部节点、驳回目标必须严格支配。
//
// 条件表达式本身的语法校验不在此处：它依赖引擎注入的求值器，由 Engine.SaveDefinition 负责。
func ValidateDefinition(def *Definition) error {
	if err := validateDefinition(def); err != nil {
		return err
	}
	return nil
}

func validateDefinition(def *Definition) errors.IAppError {
	if def == nil {
		return errors.NewCode(errors.InvalidInput, "workflow definition is nil")
	}
	if strings.TrimSpace(def.ID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow definition id is empty")
	}
	if len(def.Nodes) == 0 {
		return errors.NewCode(errors.InvalidInput, "workflow definition has no nodes").
			WithContext("definition_id", def.ID)
	}
	if strings.TrimSpace(def.StartNodeID) == "" {
		return errors.NewCode(errors.InvalidInput, "workflow start node id is empty").
			WithContext("definition_id", def.ID)
	}

	nodes := make(map[string]Node, len(def.Nodes))
	for i, node := range def.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return errors.NewCode(errors.InvalidInput, "workflow node id is empty").
				WithContext("definition_id", def.ID).
				WithContext("node_index", i)
		}
		if !isValidNodeKind(node.Kind) {
			return errors.NewCode(errors.InvalidInput, "workflow node kind is invalid").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID).
				WithContext("node_kind", string(node.Kind))
		}
		if node.Timeout < 0 {
			return errors.NewCode(errors.InvalidInput, "workflow node timeout cannot be negative").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID)
		}
		if _, exists := nodes[node.ID]; exists {
			return errors.NewCode(errors.InvalidInput, "workflow node id must be unique").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID)
		}
		nodes[node.ID] = node
	}
	if _, ok := nodes[def.StartNodeID]; !ok {
		return errors.NewCode(errors.InvalidInput, "workflow start node does not exist").
			WithContext("definition_id", def.ID).
			WithContext("start_node_id", def.StartNodeID)
	}

	incoming := make(map[string]int, len(def.Nodes))
	for _, node := range def.Nodes {
		for _, edge := range node.Edges {
			incoming[edge.Target]++
		}
	}
	if incoming[def.StartNodeID] > 0 {
		return errors.NewCode(errors.InvalidInput, "workflow start node cannot have incoming edges").
			WithContext("definition_id", def.ID).
			WithContext("start_node_id", def.StartNodeID).
			WithContext("incoming_count", incoming[def.StartNodeID])
	}
	for _, node := range def.Nodes {
		if incoming[node.ID] > 1 && node.Kind == "" {
			return errors.NewCode(errors.InvalidInput, "workflow node kind is required for multiple incoming edges").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID).
				WithContext("incoming_count", incoming[node.ID])
		}
	}

	for _, node := range def.Nodes {
		seenNext := make(map[string]struct{}, len(node.Edges))
		defaultEdges := 0
		conditionalEdges := 0
		unconditionalEdges := 0
		for _, edge := range node.Edges {
			if strings.TrimSpace(edge.Target) == "" {
				return errors.NewCode(errors.InvalidInput, "workflow next node id is empty").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID)
			}
			if _, exists := nodes[edge.Target]; !exists {
				return errors.NewCode(errors.InvalidInput, "workflow next node does not exist").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("next_node_id", edge.Target)
			}
			if _, exists := seenNext[edge.Target]; exists {
				return errors.NewCode(errors.InvalidInput, "workflow node next targets must be unique").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("next_node_id", edge.Target)
			}
			seenNext[edge.Target] = struct{}{}

			switch {
			case edge.Default:
				if strings.TrimSpace(edge.Condition) != "" {
					return errors.NewCode(errors.InvalidInput, "workflow default edge cannot carry a condition").
						WithContext("definition_id", def.ID).
						WithContext("node_id", node.ID).
						WithContext("next_node_id", edge.Target)
				}
				defaultEdges++
			case strings.TrimSpace(edge.Condition) != "":
				conditionalEdges++
			default:
				unconditionalEdges++
			}
		}
		if defaultEdges > 1 {
			return errors.NewCode(errors.InvalidInput, "workflow node can declare at most one default edge").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID).
				WithContext("default_edges", defaultEdges)
		}
		// 全部出边都带条件时，只要业务数据落在所有条件之外，实例就会推进到"无处可去"。
		// 引擎无法证明条件集合互斥且穷尽，因此要求作者显式声明兜底分支，把运行期事故前移到定义期。
		if conditionalEdges > 0 && unconditionalEdges == 0 && defaultEdges == 0 {
			return errors.NewCode(errors.InvalidInput, "workflow conditional branch requires a default edge").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID).
				WithContext("conditional_edges", conditionalEdges)
		}
	}

	visited := make(map[string]int, len(def.Nodes))
	stack := make(map[string]bool, len(def.Nodes))
	if err := walkNodeGraph(def.StartNodeID, nodes, visited, stack); err != nil {
		return err.WithContext("definition_id", def.ID)
	}
	if len(visited) != len(def.Nodes) {
		for _, node := range def.Nodes {
			if visited[node.ID] == 0 {
				return errors.NewCode(errors.InvalidInput, "workflow node is unreachable from start").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID)
			}
		}
	}
	if err := validateRejectTargets(def, nodes, buildDominanceIndex(def.StartNodeID, nodes)); err != nil {
		return err
	}
	return nil
}

func isValidNodeKind(kind NodeKind) bool {
	switch kind {
	case "", NodeKindTask, NodeKindBranch, NodeKindJoin:
		return true
	default:
		return false
	}
}

func validateRejectTargets(def *Definition, nodes map[string]Node, idx *dominanceIndex) errors.IAppError {
	for _, node := range def.Nodes {
		if node.ID == def.StartNodeID && len(node.RejectTo) > 0 {
			return errors.NewCode(errors.InvalidInput, "workflow start node cannot have reject targets").
				WithContext("definition_id", def.ID).
				WithContext("node_id", node.ID)
		}
		seenRejects := make(map[string]struct{}, len(node.RejectTo))
		for _, targetNodeID := range node.RejectTo {
			if strings.TrimSpace(targetNodeID) == "" {
				return errors.NewCode(errors.InvalidInput, "workflow reject target node id is empty").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID)
			}
			if _, exists := nodes[targetNodeID]; !exists {
				return errors.NewCode(errors.InvalidInput, "workflow reject target node does not exist").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("reject_to", targetNodeID)
			}
			if _, exists := seenRejects[targetNodeID]; exists {
				return errors.NewCode(errors.InvalidInput, "workflow reject targets must be unique").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("reject_to", targetNodeID)
			}
			seenRejects[targetNodeID] = struct{}{}
			if !strictDominatesNodeWithIndex(def.StartNodeID, targetNodeID, node.ID, idx, nodes) {
				return errors.NewCode(errors.InvalidInput, "workflow reject target must dominate node").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("reject_to", targetNodeID)
			}
		}
	}
	return nil
}
