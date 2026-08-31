package workflow

import (
	"context"
	"testing"

	"gochen/errors"
	"gochen/testkit/require"
)

func TestValidateDefinition_ConditionalBranchRequiresDefaultEdge(t *testing.T) {
	// 全部出边都带条件时，业务数据落在条件之外就会无处可去，必须在定义期拒绝。
	def := &Definition{
		ID:          "no-default",
		StartNodeID: "gate",
		Nodes: []Node{
			{ID: "gate", Edges: []Edge{
				NewConditionalEdge("high", "amount > 1000"),
				NewConditionalEdge("low", "amount < 100"),
			}},
			{ID: "high"},
			{ID: "low"},
		},
	}

	err := ValidateDefinition(def)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
	require.ErrorContains(t, err, "default edge")
}

func TestValidateDefinition_DefaultEdgeRules(t *testing.T) {
	// 默认边不得携带条件。
	withCondition := &Definition{
		ID:          "default-with-condition",
		StartNodeID: "gate",
		Nodes: []Node{
			{ID: "gate", Edges: []Edge{
				NewConditionalEdge("high", "amount > 1000"),
				{Target: "low", Condition: "amount <= 1000", Default: true},
			}},
			{ID: "high"},
			{ID: "low"},
		},
	}
	err := ValidateDefinition(withCondition)
	require.Error(t, err)
	require.ErrorContains(t, err, "default edge cannot carry a condition")

	// 同一节点最多一条默认边。
	twoDefaults := &Definition{
		ID:          "two-defaults",
		StartNodeID: "gate",
		Nodes: []Node{
			{ID: "gate", Edges: []Edge{
				NewDefaultEdge("high"),
				NewDefaultEdge("low"),
			}},
			{ID: "high"},
			{ID: "low"},
		},
	}
	err = ValidateDefinition(twoDefaults)
	require.Error(t, err)
	require.ErrorContains(t, err, "at most one default edge")
}

func TestValidateDefinition_UnconditionalEdgeSatisfiesFallback(t *testing.T) {
	// 无条件边总是激活，本身即兜底，无需再声明默认边。
	def := &Definition{
		ID:          "mixed",
		StartNodeID: "gate",
		Nodes: []Node{
			{ID: "gate", Edges: []Edge{
				NewConditionalEdge("extra", "vip == true"),
				NewEdge("always"),
			}},
			{ID: "extra"},
			{ID: "always"},
		},
	}
	require.NoError(t, ValidateDefinition(def))
}

func TestEngine_DefaultEdgeTakenOnlyWhenNoConditionMatches(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	def := &Definition{
		ID:          "route",
		StartNodeID: "gate",
		Nodes: []Node{
			{ID: "gate", Edges: []Edge{
				NewConditionalEdge("vip", "amount > 1000"),
				NewDefaultEdge("normal"),
			}},
			{ID: "vip"},
			{ID: "normal"},
		},
	}
	require.NoError(t, engine.SaveDefinition(ctx, def))

	// 条件命中：默认边不参与。
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("r1"), "route", map[string]any{"amount": 5000}))
	require.NoError(t, engine.StartInstance(ctx, ID("r1")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("r1"), "gate"))
	st, err := store.Get(ctx, ID("r1"))
	require.NoError(t, err)
	require.Equal(t, []string{"vip"}, st.ActiveNodeIDs)

	// 条件不命中：走默认边，而不是被静默判定为 completed。
	require.NoError(t, engine.CreateInstanceWithData(ctx, ID("r2"), "route", map[string]any{"amount": 10}))
	require.NoError(t, engine.StartInstance(ctx, ID("r2")))
	require.NoError(t, engine.AdvanceNode(ctx, ID("r2"), "gate"))
	st, err = store.Get(ctx, ID("r2"))
	require.NoError(t, err)
	require.Equal(t, []string{"normal"}, st.ActiveNodeIDs)
	require.Equal(t, InstanceStatusRunning, st.Status)
	require.True(t, st.CompletedAt.IsZero())
}

func TestResolveNextNodes_DeadEndIsAnError(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	// 兜底防线：即使绕过定义校验（例如存储中被直接写入非法定义），
	// 条件全不命中也必须报错，而不是返回空后继让实例被判定为 completed。
	_, err := engine.resolveNextNodes(ctx, []Edge{
		NewConditionalEdge("a", "amount > 1000"),
		NewConditionalEdge("b", "amount < 100"),
	}, map[string]any{"amount": 500}, nil)

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
	require.ErrorContains(t, err, "no outgoing edge matched")
}

func TestResolveNextNodes_TerminalNodeReturnsNoSuccessor(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	// 终结节点没有出边，必须与"条件不命中"区分开：正常返回空后继，让实例收敛。
	next, err := engine.resolveNextNodes(ctx, nil, nil, nil)
	require.NoError(t, err)
	require.Empty(t, next)
}
