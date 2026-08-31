package jsoncodec

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"

	"gochen/errors"
)

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
