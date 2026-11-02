package audited

import (
	"bytes"
	"encoding/json"
	"reflect"

	"gochen/domain/meta"
	"gochen/errors"
)

// DimensionSnapshot 保存写入时独立且只读的维度事实。
type DimensionSnapshot struct{ values map[meta.Dimension]any }

// NewDimensionSnapshot 按字段类型转换完整维度值；缺失键明确报错。
func NewDimensionSnapshot(metadata *meta.Metadata, values map[meta.Dimension]any) (DimensionSnapshot, error) {
	if metadata == nil {
		return DimensionSnapshot{}, errors.NewCode(errors.InvalidInput, "snapshot metadata is required")
	}
	out := DimensionSnapshot{values: map[meta.Dimension]any{}}
	for dimension := range values {
		if _, err := metadata.Resolve(meta.ByDimension(dimension)); err != nil {
			return DimensionSnapshot{}, err
		}
	}
	for _, field := range metadata.Fields() {
		if field.Dimension == "" {
			continue
		}
		value, exists := values[field.Dimension]
		if !exists {
			return DimensionSnapshot{}, errors.NewCode(errors.InvalidInput, "dimension snapshot key is missing").WithContext("dimension", string(field.Dimension))
		}
		converted, err := field.Convert(value)
		if err != nil {
			return DimensionSnapshot{}, err
		}
		out.values[field.Dimension] = scalarValue(converted)
	}
	return out, nil
}

// CaptureDimensions 捕获实体的完整真实维度值。
func CaptureDimensions(metadata *meta.Metadata, entity any) (DimensionSnapshot, error) {
	values := map[meta.Dimension]any{}
	for _, field := range metadata.Fields() {
		if field.Dimension == "" {
			continue
		}
		value, err := field.Read(entity)
		if err != nil {
			return DimensionSnapshot{}, err
		}
		values[field.Dimension] = value
	}
	return NewDimensionSnapshot(metadata, values)
}

// Values 返回独立的标量值副本。
func (s DimensionSnapshot) Values() map[meta.Dimension]any {
	out := make(map[meta.Dimension]any, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out
}

// Value 返回维度事实；存在且为 NULL 时返回 nil, true。
func (s DimensionSnapshot) Value(d meta.Dimension) (any, bool) {
	value, ok := s.values[d]
	return value, ok
}

// MarshalJSON 序列化维度事实。
func (s DimensionSnapshot) MarshalJSON() ([]byte, error) { return json.Marshal(s.Values()) }

// UnmarshalJSON 读取持久化标量；资源类型校验由 NewDimensionSnapshot 完成。
func (s *DimensionSnapshot) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var values map[meta.Dimension]any
	if err := decoder.Decode(&values); err != nil {
		return err
	}
	for dim, value := range values {
		if !dim.IsValid() {
			return errors.NewCode(errors.InvalidInput, "invalid snapshot dimension")
		}
		switch value.(type) {
		case nil, string, bool, json.Number:
		default:
			return errors.NewCode(errors.InvalidInput, "snapshot value must be scalar")
		}
	}
	s.values = values
	return nil
}

// Source 绑定资源字段类型和实体标识，用于历史事实求值。
func (s DimensionSnapshot) Source(metadata *meta.Metadata, entityID string) SnapshotSource {
	return SnapshotSource{snapshot: s, metadata: metadata, entityID: entityID}
}

// SnapshotSource 实现实体历史维度的规约值来源。
type SnapshotSource struct {
	snapshot DimensionSnapshot
	metadata *meta.Metadata
	entityID string
}

// Read 读取实体标识或维度；载荷字段和缺失快照键明确失败。
func (s SnapshotSource) Read(ref meta.Ref) (meta.Field, any, error) {
	field, err := s.metadata.Resolve(ref)
	if err != nil {
		return meta.Field{}, nil, err
	}
	var raw any
	if field.Identity {
		raw = s.entityID
	} else {
		if field.Dimension == "" {
			return meta.Field{}, nil, errors.NewCode(errors.InvalidInput, "audit condition must reference identity or dimension")
		}
		var exists bool
		raw, exists = s.snapshot.Value(field.Dimension)
		if !exists {
			return meta.Field{}, nil, errors.NewCode(errors.Internal, "audit dimension snapshot key is missing").WithContext("dimension", string(field.Dimension))
		}
	}
	value, err := field.Convert(raw)
	if err != nil {
		return meta.Field{}, nil, err
	}
	return field, scalarValue(value), nil
}

func scalarValue(value reflect.Value) any {
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	return value.Interface()
}
