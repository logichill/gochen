package orm

import "reflect"

// NewModel 创建模型。
func (m *ModelMeta) NewModel() any {
	if m == nil {
		return nil
	}
	if m.ModelFactory != nil {
		return m.ModelFactory()
	}
	return nil
}

// NewModelFactory 创建模型Factory。
func NewModelFactory[T any]() ModelFactory {
	typ := reflect.TypeOf((*T)(nil)).Elem()
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Struct {
		return func() any { return reflect.New(typ).Interface() }
	}
	// 保底：返回 new(T)，避免返回 nil（由调用方自行决定是否支持该类型）。
	return func() any { return new(T) }
}

// NewModelFactoryFromSample 创建模型Factory从样本。
func NewModelFactoryFromSample(sample any) ModelFactory {
	typ := reflect.TypeOf(sample)
	if typ == nil {
		return nil
	}
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	return func() any { return reflect.New(typ).Interface() }
}
