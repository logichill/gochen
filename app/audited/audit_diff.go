package audited

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"gochen/codec/jsoncodec"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// computeEntityDiff 计算两个实体之间的字段级差异，返回变化字段的 diff 列表。
// 使用 JSON 序列化比较，过滤框架元数据字段，仅保留业务字段变更。
func computeEntityDiff[T any](before, after T) (json.RawMessage, error) {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "marshal audit diff before snapshot")
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "marshal audit diff after snapshot")
	}

	beforeMap, err := unmarshalWithNumber(beforeJSON)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "decode audit diff before snapshot")
	}
	afterMap, err := unmarshalWithNumber(afterJSON)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "decode audit diff after snapshot")
	}

	// 收集所有 key 并排序，保证遍历顺序确定性
	keySet := make(map[string]struct{})
	for k := range afterMap {
		keySet[k] = struct{}{}
	}
	for k := range beforeMap {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var diffs []domaudited.FieldChange
	for _, key := range keys {
		// 过滤框架元数据字段，仅保留业务字段变更
		if isAuditIgnoredField(key) {
			continue
		}

		oldVal, oldExists := beforeMap[key]
		newVal, newExists := afterMap[key]

		if !oldExists && newExists {
			// 新增字段；但 JSON null 与缺失语义等价（无值），
			// 记录 Old/New 均为 nil 的伪变更只会产生审计噪音。
			if newVal == nil {
				continue
			}
			diffs = append(diffs, domaudited.FieldChange{Field: key, Old: nil, New: newVal})
		} else if oldExists && !newExists {
			// 删除字段；同样跳过值为 null 的伪变更。
			if oldVal == nil {
				continue
			}
			diffs = append(diffs, domaudited.FieldChange{Field: key, Old: oldVal, New: nil})
		} else if !reflect.DeepEqual(oldVal, newVal) {
			// 变更字段
			diffs = append(diffs, domaudited.FieldChange{
				Field: key,
				Old:   oldVal,
				New:   newVal,
			})
		}
	}

	if len(diffs) == 0 {
		return nil, nil
	}

	result, err := json.Marshal(diffs)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "marshal audit diff")
	}
	return result, nil
}

// unmarshalWithNumber 解码带Number。
func unmarshalWithNumber(data []byte) (map[string]any, error) {
	m, err := jsoncodec.Decode[map[string]any](data)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// auditIgnoredFields 定义审计 diff 中应忽略的框架元数据字段。
// 这些字段由框架自动维护，每次 Update 都会变化，会造成审计噪音。
var auditIgnoredFields = map[string]struct{}{
	"id":         {},
	"version":    {},
	"created_at": {},
	"created_by": {},
	"updated_at": {},
	"updated_by": {},
	"deleted_at": {},
	"deleted_by": {},
	// JSON tag 可能与 Go 字段名不同，同时支持 PascalCase 与小驼峰命名。
	"ID":        {},
	"Version":   {},
	"CreatedAt": {},
	"CreatedBy": {},
	"UpdatedAt": {},
	"UpdatedBy": {},
	"DeletedAt": {},
	"DeletedBy": {},
	"createdAt": {},
	"createdBy": {},
	"updatedAt": {},
	"updatedBy": {},
	"deletedAt": {},
	"deletedBy": {},
}

// isAuditIgnoredField 判断审计Ignored字段。
//
// 同时做归一化比对（去掉下划线并小写），这样 createdAt / created_at /
// CREATEDAT 等不同序列化风格都能被识别为同一元数据字段。
func isAuditIgnoredField(field string) bool {
	if _, ignored := auditIgnoredFields[field]; ignored {
		return true
	}
	normalized := strings.ToLower(strings.ReplaceAll(field, "_", ""))
	_, ignored := auditIgnoredFieldsNormalized[normalized]
	return ignored
}

// auditIgnoredFieldsNormalized 是 auditIgnoredFields 的归一化视图（小写、无下划线）。
var auditIgnoredFieldsNormalized = func() map[string]struct{} {
	out := make(map[string]struct{}, len(auditIgnoredFields))
	for field := range auditIgnoredFields {
		out[strings.ToLower(strings.ReplaceAll(field, "_", ""))] = struct{}{}
	}
	return out
}()

func marshalAuditSnapshotResult[T any](entity T) (json.RawMessage, error) {
	changes, err := json.Marshal(entity)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "marshal audit snapshot")
	}
	return changes, nil
}
