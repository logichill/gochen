package eventsourced

import (
	"reflect"
	"strings"

	"gochen/errors"
)

const aggregateTagName = "aggregate"

// eventSourcedAggregateBaseName 用于匹配嵌入字段的 Go 类型名。
const eventSourcedAggregateBaseName = "EventSourcedAggregate"

// ResolveAggregateType 从聚合 struct 的嵌入字段 tag 中提取 aggregateType。
//
// 扫描规则：
//   - 查找 self 的直接字段中嵌入了 EventSourcedAggregate 类型的字段；
//   - 读取该字段上的 `aggregate:"xxx"` tag 值；
//   - 仅扫描一级直接字段（不递归嵌套）。
//
// 支持指针嵌入（*EventSourcedAggregate[T]）和值嵌入（EventSourcedAggregate[T]）两种形式。
func ResolveAggregateType(self any) (string, error) {
	if self == nil {
		return "", errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil")
	}

	t := reflect.TypeOf(self)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return "", errors.NewCode(errors.InvalidInput, "aggregate sample must be a struct or pointer to struct").
			WithContext("actual_kind", t.Kind().String())
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.Anonymous {
			continue
		}

		if !isEventSourcedAggregateField(field.Type) {
			continue
		}

		return resolveAggregateTag(field, t)
	}

	return "", errors.NewCode(errors.InvalidInput, "no embedded EventSourcedAggregate field found in struct").
		WithContext("struct_type", t.String())
}

// isEventSourcedAggregateField 判断字段类型是否为 EventSourcedAggregate（含指针和泛型实例化）。
func isEventSourcedAggregateField(ft reflect.Type) bool {
	// 去掉指针层
	if ft.Kind() == reflect.Ptr {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct {
		return false
	}

	// 泛型实例化后的 Go 类型名形如 "EventSourcedAggregate[int64]"
	name := ft.Name()
	return name == eventSourcedAggregateBaseName || strings.HasPrefix(name, eventSourcedAggregateBaseName+"[")
}

// InitAggregateFromTag 创建并绑定 metadata 的事件溯源聚合根（aggregateType 从 struct tag 自动提取）。
//
// 用法：
//
//	type Account struct {
//	    *eventsourced.EventSourcedAggregate[int64] `aggregate:"account"`
//	    Balance int
//	}
//
//	func NewAccount(registry *eventsourced.MetadataRegistry, id int64) (*Account, error) {
//	    a := &Account{}
//	    agg, err := eventsourced.InitAggregateFromTag[int64](registry, a, id)
//	    if err != nil {
//	        return nil, err
//	    }
//	    a.EventSourcedAggregate = agg
//	    return a, nil
//	}
func InitAggregateFromTag[T comparable](registry *MetadataRegistry, self any, id T) (*EventSourcedAggregate[T], error) {
	aggregateType, err := ResolveAggregateType(self)
	if err != nil {
		return nil, err
	}
	return InitAggregate(registry, self, id, aggregateType)
}

// New 创建一个完全初始化的聚合实例（aggregateType 从 struct tag 自动提取，
// 嵌入的 EventSourcedAggregate 由框架自动创建并绑定 metadata）。
//
// 开发者无需手动调用显式 aggregate 初始化 helper，
// 框架会通过反射自动完成全部初始化工作。
//
// 支持指针嵌入（*EventSourcedAggregate[ID]）和值嵌入（EventSourcedAggregate[ID]）。
//
// 用法：
//
//	type Account struct {
//	    *eventsourced.EventSourcedAggregate[int64] `aggregate:"account"`
//	    Balance int
//	}
//
//	func NewAccount(registry *eventsourced.MetadataRegistry, id int64, balance int) (*Account, error) {
//	    a, err := eventsourced.New[Account, int64](registry, id)
//	    if err != nil {
//	        return nil, err
//	    }
//	    a.Balance = balance
//	    return a, nil
//	}
//
//	// 空工厂函数（用于仓储回放）可简化为一行：
//	func NewEmptyAccount(registry *eventsourced.MetadataRegistry, id int64) (*Account, error) {
//	    return eventsourced.New[Account, int64](registry, id)
//	}
func New[T any, ID comparable](registry *MetadataRegistry, id ID) (*T, error) {
	agg := new(T)
	if err := initEmbeddedAggregate(registry, agg, id); err != nil {
		return nil, err
	}
	return agg, nil
}

// initEmbeddedAggregate 通过反射找到嵌入的 EventSourcedAggregate 字段，
// 创建并初始化它，绑定 metadata，然后设置到聚合实例上。
func initEmbeddedAggregate[ID comparable](registry *MetadataRegistry, agg any, id ID) error {
	aggValue := reflect.ValueOf(agg)
	if aggValue.Kind() != reflect.Ptr || aggValue.Elem().Kind() != reflect.Struct {
		return errors.NewCode(errors.InvalidInput, "aggregate must be a pointer to struct").
			WithContext("actual_type", reflect.TypeOf(agg).String())
	}

	elemValue := aggValue.Elem()
	elemType := elemValue.Type()

	// 查找嵌入的 EventSourcedAggregate 字段
	fieldIdx := -1
	isPtr := false
	for i := 0; i < elemType.NumField(); i++ {
		f := elemType.Field(i)
		if !f.Anonymous {
			continue
		}
		if isEventSourcedAggregateField(f.Type) {
			fieldIdx = i
			isPtr = f.Type.Kind() == reflect.Ptr
			break
		}
	}
	if fieldIdx < 0 {
		return errors.NewCode(errors.InvalidInput, "no embedded EventSourcedAggregate field found").
			WithContext("struct_type", elemType.String())
	}

	// 提取 aggregate tag
	aggregateType, err := resolveTagFromField(elemType, fieldIdx)
	if err != nil {
		return err
	}

	// 创建 EventSourcedAggregate
	esa := NewEventSourcedAggregate(id, aggregateType)

	// 确保 metadata 并绑定
	meta, err := registry.Ensure(agg, aggregateType)
	if err != nil {
		return err
	}
	if err := esa.BindMetadata(agg, meta); err != nil {
		return err
	}

	// 通过反射设置嵌入字段
	field := elemValue.Field(fieldIdx)
	if isPtr {
		// 指针嵌入：直接设置指针
		esaValue := reflect.ValueOf(esa)
		if !esaValue.Type().AssignableTo(field.Type()) {
			return errors.NewCode(errors.InvalidInput, "EventSourcedAggregate ID type mismatch").
				WithContext("struct_type", elemType.String()).
				WithContext("field_type", field.Type().String()).
				WithContext("esa_type", esaValue.Type().String())
		}
		field.Set(esaValue)
	} else {
		// 值嵌入：解引用并设置
		esaValue := reflect.ValueOf(esa).Elem()
		if !esaValue.Type().AssignableTo(field.Type()) {
			return errors.NewCode(errors.InvalidInput, "EventSourcedAggregate ID type mismatch").
				WithContext("struct_type", elemType.String()).
				WithContext("field_type", field.Type().String()).
				WithContext("esa_type", esaValue.Type().String())
		}
		field.Set(esaValue)
	}

	return nil
}

// resolveTagFromField 提取指定字段的 aggregate tag 值。
func resolveTagFromField(t reflect.Type, fieldIdx int) (string, error) {
	return resolveAggregateTag(t.Field(fieldIdx), t)
}

func resolveAggregateTag(field reflect.StructField, structType reflect.Type) (string, error) {
	tagValue := field.Tag.Get(aggregateTagName)
	if tagValue == "" {
		return "", errors.NewCode(errors.InvalidInput, "embedded EventSourcedAggregate field is missing aggregate tag").
			WithContext("struct_type", structType.String()).
			WithContext("field", field.Name)
	}

	if idx := strings.IndexByte(tagValue, ','); idx >= 0 {
		tagValue = tagValue[:idx]
	}
	tagValue = strings.TrimSpace(tagValue)

	if tagValue == "" || tagValue == "-" {
		return "", errors.NewCode(errors.InvalidInput, "aggregate tag value cannot be empty or '-'").
			WithContext("struct_type", structType.String()).
			WithContext("field", field.Name)
	}
	return tagValue, nil
}
