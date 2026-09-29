package jsoncodec

import (
	"encoding/json"
	"reflect"
	"testing"

	"gochen/codec"
	"gochen/errors"

	"gochen/testkit/require"
)

func TestCodec_ImplementsRootInterface(t *testing.T) {
	var c codec.ICodec[map[string]any, []byte] = New[map[string]any]()
	require.NotNil(t, c)
}

func TestEncodeUsesDefaultCodec(t *testing.T) {
	data, err := Encode(map[string]int{"value": 1})
	require.NoError(t, err)
	require.JSONEq(t, `{"value":1}`, string(data))
}

func TestEncodeFailureIsInternal(t *testing.T) {
	_, err := Encode(make(chan int))
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Internal))
}

func TestNewIgnoresNilOptions(t *testing.T) {
	codec := New[map[string]any](nil)
	decoded, err := codec.Decode([]byte(`{"value":1}`))
	require.NoError(t, err)
	require.Equal(t, json.Number("1"), decoded["value"])
}

func TestDecode_UseNumber(t *testing.T) {
	c := New[map[string]any]()

	m, err := c.Decode([]byte(`{"id":9223372036854775807}`))
	require.NoError(t, err)

	_, ok := m["id"].(json.Number)
	require.True(t, ok, "id should be decoded as json.Number when UseNumber is enabled")
}

func TestEncode_JsonNumberIsMarshaledAsNumber(t *testing.T) {
	c := New[map[string]any]()

	m, err := c.Decode([]byte(`{"id":9223372036854775807}`))
	require.NoError(t, err)
	require.IsType(t, json.Number(""), m["id"])

	out, err := Encode(m)
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

	_, err = c.Decode([]byte(`{"a":1} x`))
	require.Error(t, err)

	_, err = c.Decode([]byte(`{"a":1} null`))
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

func TestDecodeIntoValueRejectsInaccessibleReflectValue(t *testing.T) {
	type hidden struct {
		value int
	}

	target := reflect.ValueOf(&hidden{}).Elem().FieldByName("value").Addr()
	require.False(t, target.CanInterface())
	require.NotPanics(t, func() {
		err := DecodeIntoValue([]byte(`1`), target)
		require.True(t, errors.Is(err, errors.InvalidInput))
	})
}

func TestGenericIntoFunctionsPreserveTypedContracts(t *testing.T) {
	type target struct {
		ID int `json:"id"`
	}

	var decode func([]byte, *target, ...Option) error = DecodeInto[target]
	var transcode func(any, *target, ...Option) error = TranscodeInto[target]

	var decoded target
	require.NoError(t, decode([]byte(`{"id":123}`), &decoded))
	require.Equal(t, 123, decoded.ID)

	var transcoded target
	require.NoError(t, transcode(map[string]any{"id": 456}, &transcoded))
	require.Equal(t, 456, transcoded.ID)
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

func TestTranscodeRejectsNilSource(t *testing.T) {
	_, err := Transcode[map[string]any](nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestMarshalAndTranscodePreserveErrorBoundaries(t *testing.T) {
	source := map[string]any{"unsupported": make(chan int)}

	_, err := Encode(source)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Internal))

	_, err = Transcode[map[string]any](source)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestEncodeAndTranscodeRejectCycles(t *testing.T) {
	self := make(map[string]any)
	self["self"] = self
	left := map[string]any{}
	right := map[string]any{}
	left["right"] = right
	right["left"] = left
	slice := make([]any, 1)
	slice[0] = slice
	backing := make([]any, 2)
	outer := backing[:1:2]
	inner := backing[:2:2]
	backing[0] = inner
	for name, value := range map[string]any{"map": self, "mutual_map": left, "slice": slice, "slice_views": outer} {
		t.Run(name, func(t *testing.T) {
			_, err := Encode(value)
			require.True(t, errors.Is(err, errors.Internal))
			_, err = Transcode[any](value)
			require.True(t, errors.Code(err) == errors.InvalidInput)
		})
	}
}

func TestTranscodePreservesJSONValueSemantics(t *testing.T) {
	number := json.Number("9007199254740993")
	source := map[string]any{
		"number":      &number,
		"zero":        json.Number(""),
		"nil_map":     map[string]any(nil),
		"nil_slice":   []any(nil),
		"empty_map":   map[string]any{},
		"empty_slice": []any{},
		"nested":      []any{map[string]any{"exponent": json.Number("1.25e+30")}},
	}
	data, err := Encode(source)
	require.NoError(t, err)
	require.Equal(t, `{"empty_map":{},"empty_slice":[],"nested":[{"exponent":1.25e+30}],"nil_map":null,"nil_slice":null,"number":9007199254740993,"zero":0}`, string(data))
	decoded, err := Transcode[map[string]any](source)
	require.NoError(t, err)
	require.Equal(t, json.Number("9007199254740993"), decoded["number"])
	require.Equal(t, json.Number("0"), decoded["zero"])
	require.Nil(t, decoded["nil_map"])
	require.Nil(t, decoded["nil_slice"])
	_, err = Transcode[any](map[string]any{"invalid": json.Number("01")})
	require.True(t, errors.Code(err) == errors.InvalidInput)
}
