package jsoncodec

import (
	"encoding/json"
	stderrors "errors"
	"io"
	"reflect"

	"gochen/errors"
)

// Transcode 通过 JSON 序列化/反序列化将 src 转换为目标类型 T。
func Transcode[T any](src any, opts ...Option) (T, error) {
	var target T
	if err := TranscodeInto(src, &target, opts...); err != nil {
		var zero T
		return zero, err
	}
	return target, nil
}

// TranscodeInto 通过 JSON 序列化/反序列化将 src 转换为 dest 指向的已有变量。
func TranscodeInto[T any](src any, dest *T, opts ...Option) error {
	if src == nil {
		return errors.NewCode(errors.InvalidInput, "transcode source cannot be nil")
	}
	if dest == nil {
		return errors.NewCode(errors.InvalidInput, "transcode destination cannot be nil")
	}

	data, err := MarshalPreserveNumber(src)
	if err != nil {
		return errors.Wrap(err, errors.InvalidInput, "failed to transcode JSON input")
	}

	codec := New[T](opts...)
	return codec.DecodeInto(data, dest)
}

// MarshalPreserveNumber 将 value 序列化为 JSON bytes。
// 若 value 内部包含 json.Number，会先归一化为 RawNumber，确保输出为 JSON number 而非 JSON string。
func MarshalPreserveNumber(value any) ([]byte, error) {
	normalized := NormalizeNumbers(value)
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to marshal JSON with preserved numbers").
			WithContext("mode", "preserve_number")
	}
	return data, nil
}

// NormalizeNumbers 递归扫描 value，将 json.Number 替换为 RawNumber。
func NormalizeNumbers(value any) any {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case json.Number:
		return RawNumber(v.String())
	case *json.Number:
		if v == nil {
			return nil
		}
		return RawNumber(v.String())
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = NormalizeNumbers(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = NormalizeNumbers(val)
		}
		return out
	default:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() {
			return value
		}
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return value
			}
			elem := rv.Elem()
			if elem.Kind() == reflect.Map || elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array {
				normalized := NormalizeNumbers(elem.Interface())
				return normalized
			}
		}
		return value
	}
}

func decodeWithReader(dec *json.Decoder, target any, cfg config) error {
	if cfg.useNumber {
		dec.UseNumber()
	}
	if cfg.disallowUnknownFields {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(target); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "failed to decode JSON")
	}

	if cfg.rejectTrailingData {
		var extra any
		err := dec.Decode(&extra)
		if err == nil {
			return errors.NewCode(errors.InvalidInput, "unexpected trailing data after JSON value")
		}
		if !stderrors.Is(err, io.EOF) {
			return errors.Wrap(err, errors.InvalidInput, "unexpected trailing data after JSON value")
		}
	}
	return nil
}
