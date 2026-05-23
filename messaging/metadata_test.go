package messaging_test

import (
	"encoding/json"
	"testing"

	"gochen/errors"
	"gochen/messaging"

	"github.com/stretchr/testify/require"
)

func TestMetadata_UnmarshalJSON_NonStringValueIsInvalidInput(t *testing.T) {
	var md messaging.Metadata

	err := json.Unmarshal([]byte(`{"a":1}`), &md)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestMetadata_UnmarshalJSON_InvalidJSONIsInvalidInput(t *testing.T) {
	var md messaging.Metadata

	// NOTE: encoding/json.Unmarshal won't call UnmarshalJSON when input is syntactically invalid.
	// We test the contract of Metadata.UnmarshalJSON directly.
	err := md.UnmarshalJSON([]byte(`{"a":`))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
