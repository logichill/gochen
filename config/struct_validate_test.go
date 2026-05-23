package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateTaggedStruct(t *testing.T) {
	type nested struct {
		Mode string `yaml:"mode" validate:"required,oneof=debug release test"`
	}
	type root struct {
		Port   int    `yaml:"port" validate:"min=1,max=65535"`
		Path   string `yaml:"path" validate:"required,startswith=/,nospace"`
		Nested nested `yaml:"nested"`
	}

	err := ValidateTaggedStruct(&root{
		Port: 8080,
		Path: "/api/v1",
		Nested: nested{
			Mode: "debug",
		},
	})
	require.NoError(t, err)

	err = ValidateTaggedStruct(&root{
		Port: 0,
		Path: "bad path",
		Nested: nested{
			Mode: "broken",
		},
	})
	require.Error(t, err)
}
