package jsoncodec

import (
	"encoding/json"
	"testing"

	"gochen/codec"

	"github.com/stretchr/testify/require"
)

func TestCodec_ImplementsRootInterface(t *testing.T) {
	var c codec.ICodec[map[string]any, []byte] = New[map[string]any]()
	require.NotNil(t, c)
}

func TestDecode_UseNumber(t *testing.T) {
	c := New[map[string]any]()

	m, err := c.Decode([]byte(`{"id":9223372036854775807}`))
	require.NoError(t, err)

	_, ok := m["id"].(json.Number)
	require.True(t, ok, "id should be decoded as json.Number when UseNumber is enabled")
}

func TestMarshalPreserveNumber_JsonNumberIsMarshaledAsNumber(t *testing.T) {
	c := New[map[string]any]()

	m, err := c.Decode([]byte(`{"id":9223372036854775807}`))
	require.NoError(t, err)
	require.IsType(t, json.Number(""), m["id"])

	out, err := MarshalPreserveNumber(m)
	require.NoError(t, err)
	require.Equal(t, `{"id":9223372036854775807}`, string(out))
}

func TestDecode_DisallowUnknownFields(t *testing.T) {
	type Req struct {
		A int `json:"a"`
	}

	c := New[Req](WithDisallowUnknownFields(true))

	_, err := c.Decode([]byte(`{"a":1,"b":2}`))
	require.Error(t, err)
}

func TestDecode_RejectTrailingData(t *testing.T) {
	c := New[any]()

	_, err := c.Decode([]byte(`{"a":1} {"b":2}`))
	require.Error(t, err)
}

func TestDecodeInto_DefaultOptions(t *testing.T) {
	type payload struct {
		ID int64 `json:"id"`
	}

	var out payload
	err := DecodeInto([]byte(`{"id":123}`), &out)
	require.NoError(t, err)
	require.Equal(t, int64(123), out.ID)
}

func TestRawNumber_MarshalJSON_Validate(t *testing.T) {
	var v any = map[string]any{
		"n": RawNumber("01"),
	}

	_, err := json.Marshal(v)
	require.Error(t, err)
}

func TestTranscode_MapWithJSONNumber_ToStrongType(t *testing.T) {
	type payload struct {
		ID         int64   `json:"id"`
		Points     int     `json:"points"`
		Multiplier float64 `json:"multiplier"`
	}

	out, err := Transcode[payload](map[string]any{
		"id":         json.Number("9007199254740993"),
		"points":     json.Number("12"),
		"multiplier": json.Number("1.5"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(9007199254740993), out.ID)
	require.Equal(t, 12, out.Points)
	require.Equal(t, 1.5, out.Multiplier)
}

func TestTranscodeInto_MapWithJSONNumber_ToStrongType(t *testing.T) {
	type payload struct {
		ID int64 `json:"id"`
	}

	var out payload
	err := TranscodeInto(map[string]any{"id": json.Number("42")}, &out)
	require.NoError(t, err)
	require.Equal(t, int64(42), out.ID)
}
