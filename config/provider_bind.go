package config

import (
	"reflect"
	"strings"
	"time"

	"gochen/errors"
)

// Bind 把指定前缀下的配置绑定到结构体指针。
func (p *Provider) Bind(key string, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return errors.NewCode(errors.InvalidInput, "target must be a pointer to struct")
	}

	return p.bindStruct(key, rv.Elem())
}

// bindStruct 递归把配置键绑定到结构体字段。
func (p *Provider) bindStruct(prefix string, rv reflect.Value) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fv := rv.Field(i)

		if !fv.CanSet() {
			continue
		}

		// 获取配置键名
		keyName := field.Tag.Get("config")
		if keyName == "" {
			keyName = strings.ToLower(field.Name)
		}
		if keyName == "-" {
			continue
		}

		fullKey := keyName
		if prefix != "" {
			fullKey = prefix + "." + keyName
		}

		// 递归处理嵌套结构体
		if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
			if err := p.bindStruct(fullKey, fv); err != nil {
				return err
			}
			continue
		}

		// 绑定基础类型
		if err := p.bindField(fullKey, fv); err != nil {
			return err
		}
	}

	return nil
}

// bindField 根据字段类型把单个配置值写入目标字段。
func (p *Provider) bindField(key string, fv reflect.Value) error {
	v := p.Get(key)
	if v == nil {
		return nil // 使用结构体默认值
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(p.GetString(key, ""))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == reflect.TypeOf(time.Duration(0)) {
			fv.SetInt(int64(p.GetDuration(key, 0)))
		} else {
			fv.SetInt(p.GetInt64(key, 0))
		}
	case reflect.Float32, reflect.Float64:
		fv.SetFloat(p.GetFloat64(key, 0))
	case reflect.Bool:
		fv.SetBool(p.GetBool(key, false))
	case reflect.Slice:
		if fv.Type().Elem().Kind() == reflect.String {
			slice := p.GetStringSlice(key, nil)
			fv.Set(reflect.ValueOf(slice))
		}
	}

	return nil
}

// Has 判断指定 key 是否存在且值非 nil。
func (p *Provider) Has(key string) bool {
	v, ok := p.settings[key]
	return ok && v != nil
}

// AllSettings 返回脱敏后的完整配置快照，适合日志和调试输出。
func (p *Provider) AllSettings() map[string]any {
	result := make(map[string]any, len(p.settings))
	for k, v := range p.settings {
		result[k] = p.redactIfSensitive(k, v)
	}
	return result
}

// AllSettingsUnsafe returns the full, unredacted settings map.
//
// Prefer AllSettings() for logging/debug output to avoid leaking secrets.
func (p *Provider) AllSettingsUnsafe() map[string]any {
	result := make(map[string]any, len(p.settings))
	for k, v := range p.settings {
		result[k] = v
	}
	return result
}
