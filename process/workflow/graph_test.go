package workflow

import (
	"testing"

	"gochen/errors"
	"gochen/testkit/require"
)

func TestWalkNodeGraphRejectsSelfLoop(t *testing.T) {
	nodes := map[string]Node{
		"start": {ID: "start", Edges: NewEdges("start")},
	}

	err := walkNodeGraph("start", nodes, map[string]int{}, map[string]bool{})

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestWalkNodeGraphRejectsCycle(t *testing.T) {
	nodes := map[string]Node{
		"start":  {ID: "start", Edges: NewEdges("review")},
		"review": {ID: "review", Edges: NewEdges("approve")},
		"approve": {
			ID:    "approve",
			Edges: NewEdges("review"),
		},
	}

	err := walkNodeGraph("start", nodes, map[string]int{}, map[string]bool{})

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestDominatedReachableNodesExcludesMergeWithAlternatePath(t *testing.T) {
	def := &Definition{
		ID:          "branch-join",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("split")},
			{ID: "split", Edges: NewEdges("left", "right")},
			{ID: "left", Edges: NewEdges("join")},
			{ID: "right", Edges: NewEdges("join")},
			{ID: "join", Edges: NewEdges("done")},
			{ID: "done"},
		},
	}

	got := dominatedReachableNodes(def, "left")

	require.Contains(t, got, "left")
	require.NotContains(t, got, "join")
	require.NotContains(t, got, "done")
}

func TestDominatedReachableNodesSkipsIsolatedNodes(t *testing.T) {
	def := &Definition{
		ID:          "isolated",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("review")},
			{ID: "review"},
			{ID: "isolated"},
		},
	}

	got := dominatedReachableNodes(def, "start")

	require.Contains(t, got, "start")
	require.Contains(t, got, "review")
	require.NotContains(t, got, "isolated")
}

func TestStrictDominatesUsesDominanceIndex(t *testing.T) {
	def := &Definition{
		ID:          "linear",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("review")},
			{ID: "review", Edges: NewEdges("approve")},
			{ID: "approve"},
		},
	}

	require.True(t, strictDominates(def, "start", "approve"))
	require.True(t, strictDominates(def, "review", "approve"))
	require.False(t, strictDominates(def, "approve", "review"))
	require.False(t, strictDominates(def, "review", "review"))
}

func TestBuildDominanceIndexReturnsNilForCycles(t *testing.T) {
	nodes := map[string]Node{
		"start":  {ID: "start", Edges: NewEdges("review")},
		"review": {ID: "review", Edges: NewEdges("start")},
	}

	require.Nil(t, buildDominanceIndex("start", nodes))
}
