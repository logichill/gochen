package workflow

import (
	"testing"
	"time"

	"gochen/errors"
	"gochen/testkit/require"
)

func TestValidateDefinitionReturnsNilForValidDefinition(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "valid",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("end")},
			{ID: "end"},
		},
	})
	require.NoError(t, err)
}

func TestValidateDefinitionRequiresKindForMultipleIncomingEdges(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "ambiguous-merge",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("left", "right")},
			{ID: "left", Edges: NewEdges("merge")},
			{ID: "right", Edges: NewEdges("merge")},
			{ID: "merge"},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestValidateDefinitionReturnsInvalidInputForBrokenDefinition(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "invalid",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("missing")},
		},
	})
	require.Error(t, err)
	require.Truef(t, errors.Is(err, errors.InvalidInput), "ValidateDefinition() error = %v, want InvalidInput", err)
}

func TestValidateDefinitionAllowsRejectToStartNode(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "reject-to-start",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("review")},
			{ID: "review", Edges: NewEdges("approve")},
			{ID: "approve", RejectTo: []string{"start"}},
		},
	})
	require.NoError(t, err)
}

func TestValidateDefinitionRejectsNegativeTimeout(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "negative-timeout",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Edges: NewEdges("end"), Timeout: -1 * time.Second},
			{ID: "end"},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestValidateDefinitionRejectsInvalidRejectTargets(t *testing.T) {
	tests := []struct {
		name string
		def  *Definition
	}{
		{
			name: "blank target",
			def: &Definition{
				ID:          "blank-reject-target",
				StartNodeID: "start",
				Nodes: []Node{
					{ID: "start", Edges: NewEdges("review")},
					{ID: "review", RejectTo: []string{" "}},
				},
			},
		},
		{
			name: "missing target",
			def: &Definition{
				ID:          "missing-reject-target",
				StartNodeID: "start",
				Nodes: []Node{
					{ID: "start", Edges: NewEdges("review")},
					{ID: "review", RejectTo: []string{"missing"}},
				},
			},
		},
		{
			name: "start node has reject target",
			def: &Definition{
				ID:          "start-reject-target",
				StartNodeID: "start",
				Nodes: []Node{
					{ID: "start", Edges: NewEdges("review"), RejectTo: []string{"review"}},
					{ID: "review"},
				},
			},
		},
		{
			name: "target does not dominate node",
			def: &Definition{
				ID:          "non-dominating-reject-target",
				StartNodeID: "start",
				Nodes: []Node{
					{ID: "start", Edges: NewEdges("left", "right")},
					{ID: "left", Edges: NewEdges("join")},
					{ID: "right", Edges: NewEdges("join")},
					{ID: "join", Kind: NodeKindJoin, RejectTo: []string{"left"}},
				},
			},
		},
		{
			name: "duplicate target",
			def: &Definition{
				ID:          "duplicate-reject-target",
				StartNodeID: "start",
				Nodes: []Node{
					{ID: "start", Edges: NewEdges("review")},
					{ID: "review", RejectTo: []string{"start", "start"}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDefinition(tt.def)
			require.Error(t, err)
			require.Truef(t, errors.Is(err, errors.InvalidInput), "ValidateDefinition() error = %v, want InvalidInput", err)
		})
	}
}
