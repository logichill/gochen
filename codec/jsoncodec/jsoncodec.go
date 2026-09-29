// Package jsoncodec 提供基于 encoding/json 的泛型序列化与反序列化实现。
//
// 设计原则：
// 1. 泛型安全：避免运行时类型断言失败；
// 2. 默认防御：默认使用 UseNumber 防止大整数精度丢失；默认拒绝尾随脏数据；
// 3. 错误归一：所有反序列化错误统一包装为 InvalidInput 错误码。
package jsoncodec

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"gochen/codec"
	"gochen/errors"
)

// Codec 实现 codec.ICodec[T, []byte] 泛型接口。
type Codec[T any] struct {
	cfg config
}

// New 创建带有默认防御性配置的 JSON Codec[T]。
func New[T any](opts ...Option) *Codec[T] {
	c := &Codec[T]{
		cfg: config{
			useNumber:             true,
			rejectTrailingData:    true,
			disallowUnknownFields: false,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&c.cfg)
		}
	}
	return c
}

// Encode 将对象序列化为 JSON 字节切片。
func (c *Codec[T]) Encode(v T) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to encode JSON")
	}
	return data, nil
}

// Encode 使用默认配置将强类型值序列化为 JSON 字节切片。
// 遵循 encoding/json 语义：json.Number 保留数字表示，零值编码为 0；
// nil map/slice 编码为 null，循环引用返回 Internal 错误。
func Encode[T any](v T) ([]byte, error) {
	return New[T]().Encode(v)
}

// Decode 将 JSON 字节切片反序列化为目标类型 T。
func (c *Codec[T]) Decode(data []byte) (T, error) {
	var target T
	if err := c.DecodeInto(data, &target); err != nil {
		var zero T
		return zero, err
	}
	return target, nil
}

// DecodeInto 将 JSON 字节切片反序列化到指定的已分配变量中。
func (c *Codec[T]) DecodeInto(data []byte, target *T) error {
	if target == nil {
		return errors.NewCode(errors.InvalidInput, "decode target cannot be nil")
	}
	return c.decodeIntoValue(data, target)
}

func (c *Codec[T]) decodeIntoValue(data []byte, target any) error {
	if target == nil {
		return errors.NewCode(errors.InvalidInput, "decode target cannot be nil")
	}
	if v, ok := target.(reflect.Value); ok {
		if !v.IsValid() {
			return errors.NewCode(errors.InvalidInput, "decode target cannot be nil")
		}
		if v.Kind() != reflect.Pointer {
			return errors.NewCode(errors.InvalidInput, "decode target must be a pointer")
		}
		if v.IsNil() {
			return errors.NewCode(errors.InvalidInput, "decode target cannot be nil")
		}
		if !v.CanInterface() {
			return errors.NewCode(errors.InvalidInput, "decode target is not accessible")
		}
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			return errors.NewCode(errors.InvalidInput, "cannot decode empty JSON payload")
		}
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		return decodeWithReader(dec, v.Interface(), c.cfg)
	}
	if reflect.TypeOf(target).Kind() != reflect.Pointer {
		return errors.NewCode(errors.InvalidInput, "decode target must be a pointer")
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.NewCode(errors.InvalidInput, "cannot decode empty JSON payload")
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	return decodeWithReader(dec, target, c.cfg)
}

// Decode 使用默认（或指定 Option）配置反序列化 JSON 字节切片到类型 T。
func Decode[T any](data []byte, opts ...Option) (T, error) {
	c := New[T](opts...)
	return c.Decode(data)
}

// DecodeInto 使用默认配置反序列化 JSON 字节切片到目标变量中。
func DecodeInto[T any](data []byte, target *T, opts ...Option) error {
	c := New[T](opts...)
	return c.DecodeInto(data, target)
}

// DecodeIntoValue 使用指定配置反序列化 JSON 字节切片到目标变量中（支持 any / pointer / reflect.Value）。
func DecodeIntoValue(data []byte, target any, opts ...Option) error {
	c := New[any](opts...)
	return c.decodeIntoValue(data, target)
}

// DecodeReader 从 io.Reader 读取并反序列化 JSON 数据。
func (c *Codec[T]) DecodeReader(r io.Reader) (T, error) {
	var target T
	if r == nil {
		return target, errors.NewCode(errors.InvalidInput, "reader cannot be nil")
	}

	dec := json.NewDecoder(r)
	if err := decodeWithReader(dec, &target, c.cfg); err != nil {
		var zero T
		return zero, err
	}
	return target, nil
}

// 确保 Codec[T] 实现了 codec.ICodec[T, []byte] 接口。
var _ codec.ICodec[any, []byte] = (*Codec[any])(nil)
