// Package meta 提供实体字段描述、维度绑定与统一类型转换。
package meta

import "gochen/errors"

// Dimension 表示项目定义的字段语义维度。
type Dimension string

// IsValid 校验维度名称的小写标识符语法。
func (d Dimension) IsValid() bool {
	if len(d) == 0 || d[0] < 'a' || d[0] > 'z' {
		return false
	}
	for i := 1; i < len(d); i++ {
		c := d[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ParseDimension 严格解析 dim 标签。
func ParseDimension(raw string) (Dimension, error) {
	d := Dimension(raw)
	if !d.IsValid() {
		return "", errors.NewCode(errors.InvalidInput, "invalid dimension name").WithContext("dimension", raw)
	}
	return d, nil
}
