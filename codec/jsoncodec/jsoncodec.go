// Package jsoncodec 提供基于 JSON 文本载体的 codec 实现与数字保持辅助函数。
package jsoncodec

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"

	"gochen/errors"
)

// Option 表示 JSON codec 的可选项。
type Option func(c *config)

type config struct {
	useNumber             bool
	rejectTrailingData    bool
	disallowUnknownFields bool
}

func WithUseNumber(enable bool) Option {
	return func(c *config) {
		c.useNumber = enable
	}
}

// WithRejectTrailingData 设置是否拒绝尾随数据（默认启用）。
func WithRejectTrailingData(enable bool) Option {
	return func(c *config) {
		c.rejectTrailingData = enable
	}
}

// WithDisallowUnknownFields 设置是否拒绝未知字段（默认关闭）。
func WithDisallowUnknownFields(enable bool) Option {
	return func(c *config) {
		c.disallowUnknownFields = enable
	}
}

// Codec 表示基于 JSON 文本的泛型 codec。
type Codec[T any] struct {
	config config
}

// New 创建Codec[T]。
func New[T any](opts ...Option) *Codec[T] {
	cfg := config{
		useNumber:          true,
		rejectTrailingData: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Codec[T]{config: cfg}
}

// Encode 将强类型值编码为 JSON bytes。
func (c *Codec[T]) Encode(v T) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to serialize JSON")
	}
	return data, nil
}

// Encode 使用默认选项将强类型值编码为 JSON bytes。
func Encode[T any](v T) ([]byte, error) {
	return New[T]().Encode(v)
}

// Decode 将 JSON bytes 解码为强类型值。
func (c *Codec[T]) Decode(data []byte) (T, error) {
	var zero T
	target := new(T)
	if err := c.decodeInto(data, target); err != nil {
		return zero, err
	}
	return *target, nil
}

// decodeInto 解码Into。
func (c *Codec[T]) decodeInto(data []byte, target any) error {
	if target == nil {
		return errors.NewCode(errors.InvalidInput, "decode target cannot be nil")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	if c.config.useNumber {
		dec.UseNumber()
	}
	if c.config.disallowUnknownFields {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(target); err != nil {
		return errors.Wrap(err, errors.InvalidInput, "failed to parse JSON")
	}
	if c.config.rejectTrailingData {
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				return errors.NewCode(errors.InvalidInput, "failed to parse JSON: trailing data")
			}
			return errors.Wrap(err, errors.InvalidInput, "failed to parse JSON: trailing data")
		}
	}
	return nil
}

// Decode 使用默认选项将 JSON bytes 解码为强类型值。
func Decode[T any](data []byte, opts ...Option) (T, error) {
	return New[T](opts...).Decode(data)
}

// DecodeInto 使用默认选项将 JSON bytes 解码到目标对象。
func DecodeInto[T any](data []byte, out *T, opts ...Option) error {
	if out == nil {
		return errors.NewCode(errors.InvalidInput, "decode output cannot be nil")
	}
	decoded, err := Decode[T](data, opts...)
	if err != nil {
		return err
	}
	*out = decoded
	return nil
}

// Transcode 将任意 JSON 兼容对象重新编码并解码为强类型值。
//
// 说明：
// - 主要用于 any/map[string]any 中间态回填到 struct 时，统一复用 PreserveNumber 语义；
// - 输入为 nil 时返回 InvalidInput，避免静默转成零值对象。
func Transcode[T any](input any, opts ...Option) (T, error) {
	var zero T
	if input == nil {
		return zero, errors.NewCode(errors.InvalidInput, "transcode input cannot be nil")
	}
	b, err := MarshalPreserveNumber(input)
	if err != nil {
		return zero, errors.Wrap(err, errors.InvalidInput, "failed to transcode JSON input")
	}
	return Decode[T](b, opts...)
}

// TranscodeInto 将任意 JSON 兼容对象重新编码并解码到目标对象。
func TranscodeInto[T any](input any, out *T, opts ...Option) error {
	if out == nil {
		return errors.NewCode(errors.InvalidInput, "transcode output cannot be nil")
	}
	decoded, err := Transcode[T](input, opts...)
	if err != nil {
		return err
	}
	*out = decoded
	return nil
}

// RawNumber 表示“以字符串保存原始表示，但在 JSON 中按 number 输出”的数值类型。
//
// 说明：
// - 主要用于解决 json.Number 在 any/map 中间态再次 Marshal 时被编码为 JSON string 的问题；
// - RawNumber.MarshalJSON 会输出不带引号的数字 token，并做语法校验以避免注入。
type RawNumber string

var jsonNumberRE = regexp.MustCompile(`^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$`)

// MarshalJSON 编码JSON。
func (n RawNumber) MarshalJSON() ([]byte, error) {
	s := string(n)
	if s == "" {
		return nil, errors.NewCode(errors.InvalidInput, "raw number is empty")
	}
	if !jsonNumberRE.MatchString(s) {
		return nil, errors.NewCode(errors.InvalidInput, "invalid JSON number").WithContext("value", s)
	}
	return []byte(s), nil
}

// UnmarshalJSON 解码JSON。
func (n *RawNumber) UnmarshalJSON(data []byte) error {
	if n == nil {
		return errors.NewCode(errors.InvalidInput, "raw number target cannot be nil")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return errors.NewCode(errors.InvalidInput, "empty JSON for raw number")
	}

	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return errors.Wrap(err, errors.InvalidInput, "failed to parse raw number string")
		}
		if s == "" {
			return errors.NewCode(errors.InvalidInput, "raw number string is empty")
		}
		if !jsonNumberRE.MatchString(s) {
			return errors.NewCode(errors.InvalidInput, "invalid JSON number").WithContext("value", s)
		}
		*n = RawNumber(s)
		return nil
	}

	s := string(trimmed)
	if !jsonNumberRE.MatchString(s) {
		return errors.NewCode(errors.InvalidInput, "invalid JSON number").WithContext("value", s)
	}
	*n = RawNumber(s)
	return nil
}

func (n RawNumber) Int64() (int64, error) {
	i, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return 0, errors.Wrap(err, errors.InvalidInput, "parse raw number as int64").WithContext("value", string(n))
	}
	return i, nil
}

func (n RawNumber) Float64() (float64, error) {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return 0, errors.Wrap(err, errors.InvalidInput, "parse raw number as float64").WithContext("value", string(n))
	}
	return f, nil
}

// NormalizeNumbers 规范化Numbers。
func NormalizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			t[k] = NormalizeNumbers(vv)
		}
		return t
	case []any:
		for i := range t {
			t[i] = NormalizeNumbers(t[i])
		}
		return t
	case json.Number:
		return RawNumber(t.String())
	default:
		return v
	}
}

// MarshalPreserveNumber 将 v 序列化为 JSON bytes，并尽力保持数字精度与数值类型。
//
// 说明：
// - 主要用于处理包含 json.Number 的 any/map 中间态：先将 json.Number 规范化为 RawNumber，再 Marshal；
// - 对于 float64/float32，本函数不改变其精度语义（浮点误差仍需业务层选择更合适的表示）。
func MarshalPreserveNumber(v any) ([]byte, error) {
	data, err := json.Marshal(NormalizeNumbers(v))
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "failed to serialize JSON").WithContext("mode", "preserve_number")
	}
	return data, nil
}
