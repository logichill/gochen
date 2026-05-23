package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Get 返回原始配置值，不做类型转换。
func (p *Provider) Get(key string) any {
	return p.settings[key]
}

// GetString 读取字符串配置；缺失或类型不匹配时返回默认值并记录 warning。
func (p *Provider) GetString(key string, defaultValue string) string {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	}

	p.warnInvalid(key, "string", v, "type mismatch; using default")
	return defaultValue
}

// GetStringStrict 获取字符串配置（严格模式）。
func (p *Provider) GetStringStrict(key string) (string, error) {
	v, err := p.getRequired(key)
	if err != nil {
		return "", err
	}

	switch val := v.(type) {
	case string:
		return val, nil
	case []byte:
		return string(val), nil
	default:
		return "", p.invalidConfig(key, "string", v)
	}
}

// GetInt 读取整数配置；缺失或类型不匹配时返回默认值并记录 warning。
func (p *Provider) GetInt(key string, defaultValue int) int {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			p.warnInvalid(key, "int", v, "failed to parse int; using default")
			return defaultValue
		}
		return i
	}

	p.warnInvalid(key, "int", v, "type mismatch; using default")
	return defaultValue
}

// GetIntStrict 获取整数配置（严格模式）。
func (p *Provider) GetIntStrict(key string) (int, error) {
	v, err := p.getRequired(key)
	if err != nil {
		return 0, err
	}

	// Derive platform int bounds for safe conversions.
	maxInt := int64(int(^uint(0) >> 1))
	minInt := -maxInt - 1

	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		if val > maxInt || val < minInt {
			return 0, p.invalidConfig(key, "int", v)
		}
		return int(val), nil
	case float64:
		if math.Trunc(val) != val {
			return 0, p.invalidConfig(key, "int", v)
		}
		if val > float64(maxInt) || val < float64(minInt) {
			return 0, p.invalidConfig(key, "int", v)
		}
		return int(val), nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			return 0, p.wrapInvalidConfig(key, "int", v, err)
		}
		return i, nil
	default:
		return 0, p.invalidConfig(key, "int", v)
	}
}

// GetInt64 读取 int64 配置；缺失或类型不匹配时返回默认值并记录 warning。
func (p *Provider) GetInt64(key string, defaultValue int64) int64 {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			p.warnInvalid(key, "int64", v, "failed to parse int64; using default")
			return defaultValue
		}
		return i
	}

	p.warnInvalid(key, "int64", v, "type mismatch; using default")
	return defaultValue
}

// GetFloat64 读取浮点配置；缺失或类型不匹配时返回默认值并记录 warning。
func (p *Provider) GetFloat64(key string, defaultValue float64) float64 {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			p.warnInvalid(key, "float64", v, "failed to parse float64; using default")
			return defaultValue
		}
		return f
	}

	p.warnInvalid(key, "float64", v, "type mismatch; using default")

	return defaultValue
}

// GetBool 读取布尔配置；缺失或类型不匹配时返回默认值并记录 warning。
func (p *Provider) GetBool(key string, defaultValue bool) bool {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case bool:
		return val
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "yes", "1", "on":
			return true
		case "false", "no", "0", "off":
			return false
		default:
			p.warnInvalid(key, "bool", v, "failed to parse bool; using default")
			return defaultValue
		}
	case int:
		return val != 0
	case int64:
		return val != 0
	}

	p.warnInvalid(key, "bool", v, "type mismatch; using default")
	return defaultValue
}

// GetBoolStrict 获取布尔配置（严格模式）。
func (p *Provider) GetBoolStrict(key string) (bool, error) {
	v, err := p.getRequired(key)
	if err != nil {
		return false, err
	}

	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "yes", "1", "on":
			return true, nil
		case "false", "no", "0", "off":
			return false, nil
		default:
			return false, p.invalidConfig(key, "bool", v)
		}
	case int:
		return val != 0, nil
	case int64:
		return val != 0, nil
	default:
		return false, p.invalidConfig(key, "bool", v)
	}
}

// GetDuration 读取 duration 配置；数值输入会按秒解释。
func (p *Provider) GetDuration(key string, defaultValue time.Duration) time.Duration {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case time.Duration:
		return val
	case string:
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			p.warnInvalid(key, "duration", v, "failed to parse duration; using default")
			return defaultValue
		}
		return d
	case int:
		return time.Duration(val) * time.Second
	case int64:
		return time.Duration(val) * time.Second
	case float64:
		return time.Duration(val * float64(time.Second))
	}

	p.warnInvalid(key, "duration", v, "type mismatch; using default")
	return defaultValue
}

// GetDurationStrict 获取时间间隔配置（严格模式）。
func (p *Provider) GetDurationStrict(key string) (time.Duration, error) {
	v, err := p.getRequired(key)
	if err != nil {
		return 0, err
	}

	switch val := v.(type) {
	case time.Duration:
		return val, nil
	case string:
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return 0, p.wrapInvalidConfig(key, "duration", v, err)
		}
		return d, nil
	case int:
		return time.Duration(val) * time.Second, nil
	case int64:
		return time.Duration(val) * time.Second, nil
	case float64:
		return time.Duration(val * float64(time.Second)), nil
	default:
		return 0, p.invalidConfig(key, "duration", v)
	}
}

// GetStringSlice 读取字符串切片配置，并兼容逗号分隔的字符串输入。
func (p *Provider) GetStringSlice(key string, defaultValue []string) []string {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, len(val))
		for i, item := range val {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result
	case string:
		// 支持逗号分隔的字符串（忽略空元素并 TrimSpace）
		parts := strings.Split(val, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			s := strings.TrimSpace(part)
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	}

	p.warnInvalid(key, "[]string", v, "type mismatch; using default")
	return defaultValue
}

// GetStringMap 读取字符串映射配置，并兼容 `map[string]any` 输入。
func (p *Provider) GetStringMap(key string, defaultValue map[string]string) map[string]string {
	v, ok := p.settings[key]
	if !ok || v == nil {
		return defaultValue
	}

	switch val := v.(type) {
	case map[string]string:
		return val
	case map[string]any:
		result := make(map[string]string, len(val))
		for k, v := range val {
			result[k] = fmt.Sprintf("%v", v)
		}
		return result
	}

	p.warnInvalid(key, "map[string]string", v, "type mismatch; using default")
	return defaultValue
}
