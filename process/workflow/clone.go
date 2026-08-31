package workflow

import (
	"time"

	"gochen/internal/clonevalue"
)

func cloneDefinition(def *Definition) *Definition {
	if def == nil {
		return nil
	}
	cp := *def
	if def.Nodes != nil {
		cp.Nodes = make([]Node, len(def.Nodes))
		for i, node := range def.Nodes {
			nodeCopy := node
			if node.Edges != nil {
				nodeCopy.Edges = make([]Edge, len(node.Edges))
				copy(nodeCopy.Edges, node.Edges)
			}
			if node.RejectTo != nil {
				nodeCopy.RejectTo = make([]string, len(node.RejectTo))
				copy(nodeCopy.RejectTo, node.RejectTo)
			}
			cp.Nodes[i] = nodeCopy
		}
	}
	return &cp
}

func cloneState(st *State) *State {
	if st == nil {
		return nil
	}
	cp := *st
	if st.ActiveNodeIDs != nil {
		cp.ActiveNodeIDs = make([]string, len(st.ActiveNodeIDs))
		copy(cp.ActiveNodeIDs, st.ActiveNodeIDs)
	}
	if st.ActiveNodeStartedAt != nil {
		cp.ActiveNodeStartedAt = make(map[string]time.Time, len(st.ActiveNodeStartedAt))
		for k, v := range st.ActiveNodeStartedAt {
			cp.ActiveNodeStartedAt[k] = v
		}
	}
	if st.PendingJoins != nil {
		cp.PendingJoins = make([]PendingJoin, len(st.PendingJoins))
		for i, join := range st.PendingJoins {
			joinCopy := join
			if join.ArrivedFrom != nil {
				joinCopy.ArrivedFrom = make([]string, len(join.ArrivedFrom))
				copy(joinCopy.ArrivedFrom, join.ArrivedFrom)
			}
			cp.PendingJoins[i] = joinCopy
		}
	}
	if st.CompletedNodeIDs != nil {
		cp.CompletedNodeIDs = make([]string, len(st.CompletedNodeIDs))
		copy(cp.CompletedNodeIDs, st.CompletedNodeIDs)
	}
	if st.History != nil {
		cp.History = make([]HistoryEntry, len(st.History))
		copy(cp.History, st.History)
	}
	if st.Data != nil {
		cp.Data = cloneWorkflowMap(st.Data)
	}
	return &cp
}

func cloneWorkflowMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for k, v := range value {
		out[k] = clonevalue.Clone(v)
	}
	return out
}
