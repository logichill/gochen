// Package idcodec 提供 ID 值与通用 raw 值之间的编解码实现。
package idcodec

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"

	"gochen/codec"
	"gochen/errors"
)

// NewDefault 创建默认。
func NewDefault[ID comparable]() (codec.ICodec[ID, any], error) {
	idType := reflect.TypeOf((*ID)(nil)).Elem()
	switch idType.Kind() {
	case reflect.Int64:
		return castCodec[ID, int64]{
			base:     NewInt64[int64](),
			idType:   idType,
			baseType: reflect.TypeOf(int64(0)),
		}, nil
	case reflect.String:
		return castCodec[ID, string]{
			base:     NewString[string](),
			idType:   idType,
			baseType: reflect.TypeOf(""),
		}, nil
	default:
		return nil, errors.NewCode(errors.Unsupported, "no default codec for id type").
			WithContext("id_type", idType.String())
	}
}

// NewInt64 创建适用于 `~int64` 的 ID codec。
func NewInt64[ID ~int64]() codec.ICodec[ID, any] { return int64Codec[ID]{} }

// NewString 创建适用于 `~string` 的 ID codec。
func NewString[ID ~string]() codec.ICodec[ID, any] { return stringCodec[ID]{} }

type castCodec[ID comparable, Base comparable] struct {
	base     codec.ICodec[Base, any]
	idType   reflect.Type
	baseType reflect.Type
}

// Encode 编码数据。
func (c castCodec[ID, Base]) Encode(id ID) (any, error) {
	if c.idType == c.baseType {
		return any(id), nil
	}
	v := reflect.ValueOf(id)
	if !v.IsValid() || !v.Type().ConvertibleTo(c.baseType) {
		return nil, errors.NewCode(errors.InvalidInput, "cannot encode id as base type").
			WithContext("id_type", c.idType.String()).
			WithContext("base_type", c.baseType.String()).
			WithContext("value_type", reflect.TypeOf(id).String())
	}
	return v.Convert(c.baseType).Interface(), nil
}

// Decode 解码数据。
func (c castCodec[ID, Base]) Decode(value any) (ID, error) {
	var zero ID
	typed, err := c.base.Decode(value)
	if err != nil {
		return zero, err
	}
	if c.idType == c.baseType {
		return any(typed).(ID), nil
	}
	v := reflect.ValueOf(typed)
	if !v.IsValid() || !v.Type().ConvertibleTo(c.idType) {
		return zero, errors.NewCode(errors.InvalidInput, "cannot convert decoded value to id type").
			WithContext("id_type", c.idType.String()).
			WithContext("decoded_type", reflect.TypeOf(typed).String())
	}
	return v.Convert(c.idType).Interface().(ID), nil
}

type int64Codec[ID ~int64] struct{}

// Encode 编码数据。
func (int64Codec[ID]) Encode(id ID) (any, error) { return int64(id), nil }

// Decode 解码数据。
func (int64Codec[ID]) Decode(value any) (ID, error) {
	var zero ID
	if value == nil {
		return zero, errors.NewCode(errors.InvalidInput, "cannot decode nil into int64 id")
	}
	switch v := value.(type) {
	case int64:
		return ID(v), nil
	case int32:
		return ID(int64(v)), nil
	case int:
		return ID(int64(v)), nil
	case uint64:
		if v > uint64(math.MaxInt64) {
			return zero, errors.NewCode(errors.InvalidInput, "uint64 overflows int64 id").WithContext("value", v)
		}
		return ID(int64(v)), nil
	case uint:
		if uint64(v) > uint64(math.MaxInt64) {
			return zero, errors.NewCode(errors.InvalidInput, "uint overflows int64 id").WithContext("value", v)
		}
		return ID(int64(v)), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return zero, errors.Wrap(err, errors.InvalidInput, "parse json.Number as int64").
				WithContext("value", string(v))
		}
		return ID(i), nil
	case []byte:
		s := strings.TrimSpace(string(v))
		if s == "" {
			return zero, errors.NewCode(errors.InvalidInput, "empty bytes for int64 id")
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return zero, errors.Wrap(err, errors.InvalidInput, "parse bytes as int64").
				WithContext("value", string(v)).
				WithContext("value_trimmed", s)
		}
		return ID(i), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return zero, errors.NewCode(errors.InvalidInput, "empty string for int64 id")
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return zero, errors.Wrap(err, errors.InvalidInput, "parse string as int64").
				WithContext("value", v).
				WithContext("value_trimmed", s)
		}
		return ID(i), nil
	default:
		return zero, errors.NewCode(errors.InvalidInput, "unsupported decode type for int64 id").
			WithContext("value_type", reflect.TypeOf(value).String())
	}
}

type stringCodec[ID ~string] struct{}

// Encode 编码数据。
func (stringCodec[ID]) Encode(id ID) (any, error) { return string(id), nil }

// Decode 解码数据。
func (stringCodec[ID]) Decode(value any) (ID, error) {
	var zero ID
	if value == nil {
		return zero, errors.NewCode(errors.InvalidInput, "cannot decode nil into string id")
	}
	switch v := value.(type) {
	case string:
		return ID(v), nil
	case []byte:
		return ID(string(v)), nil
	default:
		return zero, errors.NewCode(errors.InvalidInput, "unsupported decode type for string id").
			WithContext("value_type", reflect.TypeOf(value).String())
	}
}
