package workflow

// cloneDefinition 复制流程定义，避免调用方修改传入对象后影响存储中的定义。
func cloneDefinition(def *Definition) *Definition {
	if def == nil {
		return nil
	}
	clone := &Definition{
		ID:          def.ID,
		Name:        def.Name,
		StartNodeID: def.StartNodeID,
		CreatedAt:   def.CreatedAt,
		UpdatedAt:   def.UpdatedAt,
	}
	if def.Nodes != nil {
		clone.Nodes = make([]Node, len(def.Nodes))
		for i := range def.Nodes {
			clone.Nodes[i] = Node{
				ID:   def.Nodes[i].ID,
				Name: def.Nodes[i].Name,
				Kind: def.Nodes[i].Kind,
			}
			clone.Nodes[i].Next = cloneStrings(def.Nodes[i].Next)
		}
	}
	return clone
}

// cloneState 复制实例状态，隔离存储层与调用方之间的可变切片和 Data 字段。
func cloneState(st *State) *State {
	if st == nil {
		return nil
	}
	clone := &State{
		ID:            st.ID,
		DefinitionID:  st.DefinitionID,
		Version:       st.Version,
		Status:        st.Status,
		CurrentNodeID: st.CurrentNodeID,
		StartedAt:     st.StartedAt,
		CompletedAt:   st.CompletedAt,
		CreatedAt:     st.CreatedAt,
		UpdatedAt:     st.UpdatedAt,
	}
	clone.ActiveNodeIDs = cloneStrings(st.ActiveNodeIDs)
	clone.CompletedNodeIDs = cloneStrings(st.CompletedNodeIDs)
	if st.PendingJoins != nil {
		clone.PendingJoins = make([]PendingJoin, len(st.PendingJoins))
		for i := range st.PendingJoins {
			clone.PendingJoins[i] = PendingJoin{
				NodeID:        st.PendingJoins[i].NodeID,
				ExpectedCount: st.PendingJoins[i].ExpectedCount,
				ArrivedFrom:   cloneStrings(st.PendingJoins[i].ArrivedFrom),
			}
		}
	}
	if st.History != nil {
		clone.History = make([]HistoryEntry, len(st.History))
		copy(clone.History, st.History)
	}
	if st.Data != nil {
		clone.Data = make(map[string]any, len(st.Data))
		for key, value := range st.Data {
			clone.Data[key] = cloneWorkflowValue(value)
		}
	}
	return clone
}

// cloneWorkflowValue 递归复制 Data 中常见的可变容器类型。
func cloneWorkflowValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneWorkflowMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneWorkflowValue(typed[i])
		}
		return out
	case []string:
		return cloneStrings(typed)
	case []byte:
		out := make([]byte, len(typed))
		copy(out, typed)
		return out
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for i := range typed {
			out[i] = cloneWorkflowMap(typed[i])
		}
		return out
	default:
		return value
	}
}

// cloneWorkflowMap 复制工作流 Data map，并递归复制其中的可变值。
func cloneWorkflowMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = cloneWorkflowValue(item)
	}
	return out
}

// cloneStrings 返回字符串切片副本，保持 nil 与空切片语义不变。
func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
