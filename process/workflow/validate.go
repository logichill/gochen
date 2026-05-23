package workflow

import (
	"strings"

	"gochen/errors"
)

// ValidateDefinition 校验流程定义是从起点可达的无环 DAG。
//
// 校验项包括：定义与节点 ID 非空、节点 ID 唯一、StartNodeID 存在、Next 指向合法、
// 同一节点的后继不重复、从起点遍历无环且能覆盖全部节点。
// 导出边界显式判空，避免 typed-nil *errors.AppError 被装箱成非 nil error。
func ValidateDefinition(def *Definition) error {
	if err := validateDefinition(def); err != nil {
		return err
	}
	return nil
}

func validateDefinition(def *Definition) *errors.AppError {
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
		for _, nextNodeID := range node.Next {
			incoming[nextNodeID]++
		}
	}
	if incoming[def.StartNodeID] > 0 {
		return errors.NewCode(errors.InvalidInput, "workflow start node cannot have incoming edges").
			WithContext("definition_id", def.ID).
			WithContext("start_node_id", def.StartNodeID).
			WithContext("incoming_count", incoming[def.StartNodeID])
	}

	for _, node := range def.Nodes {
		seenNext := make(map[string]struct{}, len(node.Next))
		for _, nextNodeID := range node.Next {
			if strings.TrimSpace(nextNodeID) == "" {
				return errors.NewCode(errors.InvalidInput, "workflow next node id is empty").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID)
			}
			if _, exists := nodes[nextNodeID]; !exists {
				return errors.NewCode(errors.InvalidInput, "workflow next node does not exist").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("next_node_id", nextNodeID)
			}
			if _, exists := seenNext[nextNodeID]; exists {
				return errors.NewCode(errors.InvalidInput, "workflow node next targets must be unique").
					WithContext("definition_id", def.ID).
					WithContext("node_id", node.ID).
					WithContext("next_node_id", nextNodeID)
			}
			seenNext[nextNodeID] = struct{}{}
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
