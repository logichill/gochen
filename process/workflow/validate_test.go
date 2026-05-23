package workflow

import (
	"testing"

	"gochen/errors"
)

func TestValidateDefinitionReturnsNilForValidDefinition(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "valid",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"end"}},
			{ID: "end"},
		},
	})
	if err != nil {
		t.Fatalf("ValidateDefinition() error = %v", err)
	}
}

func TestValidateDefinitionReturnsInvalidInputForBrokenDefinition(t *testing.T) {
	err := ValidateDefinition(&Definition{
		ID:          "invalid",
		StartNodeID: "start",
		Nodes: []Node{
			{ID: "start", Next: []string{"missing"}},
		},
	})
	if err == nil {
		t.Fatal("ValidateDefinition() error = nil, want non-nil")
	}
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("ValidateDefinition() error = %v, want InvalidInput", err)
	}
}
