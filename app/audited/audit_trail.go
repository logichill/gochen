package audited

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	auth "gochen/auth"
	"gochen/codec/jsoncodec"
	"gochen/domain/audited"
	"gochen/errors"
)

// AuditTrail 返回指定实体的审计记录，支持分页。
func (s *Application[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error) {
	// 构造阶段已保证 auditStore 非空；这里保留兜底以便更清晰的错误语义。
	if s.auditStore == nil {
		return nil, errors.NewCode(errors.Internal, "auditStore is nil")
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	return s.auditStore.ListAuditRecordsByEntity(ctx, fmt.Sprint(id), offset, limit)
}

// saveAuditRecords 保存审计Records。
func (s *Application[T, ID]) saveAuditRecords(ctx context.Context, records []audited.AuditRecord) error {
	if len(records) == 0 {
		return nil
	}
	if s.auditStore == nil {
		return errors.NewCode(errors.Internal, "auditStore is nil")
	}
	if batch, ok := any(s.auditStore).(audited.IBatchAuditStore); ok && batch != nil {
		return batch.SaveAuditRecords(ctx, records)
	}
	for i := range records {
		if _, err := s.auditStore.SaveAuditRecord(ctx, records[i]); err != nil {
			return err
		}
	}
	return nil
}

// saveAudit 保存审计。
func (s *Application[T, ID]) saveAudit(ctx context.Context, id ID, op audited.AuditOperation, changes json.RawMessage) error {
	by := auth.Operator(ctx)
	if by == "" {
		// 防御性断言：调用方应已校验并注入 operator；若仍为空则为调用方 bug。
		return errors.NewCode(errors.Internal, "saveAudit: operator not found in context (caller bug)")
	}

	rec := audited.AuditRecord{
		ID:        0,
		EntityID:  fmt.Sprint(id),
		Operation: op,
		Operator:  by,
		Timestamp: time.Now(),
		Changes:   changes,
	}
	return s.saveAuditRecords(ctx, []audited.AuditRecord{rec})
}

// computeEntityDiff 计算两个实体之间的字段级差异，返回变化字段的 diff 列表。
// 使用 JSON 序列化比较，过滤框架元数据字段，仅保留业务字段变更。
func computeEntityDiff[T any](before, after T) json.RawMessage {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return nil
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return nil
	}

	beforeMap, err := unmarshalWithNumber(beforeJSON)
	if err != nil {
		return nil
	}
	afterMap, err := unmarshalWithNumber(afterJSON)
	if err != nil {
		return nil
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

	var diffs []audited.FieldChange
	for _, key := range keys {
		// 过滤框架元数据字段，仅保留业务字段变更
		if isAuditIgnoredField(key) {
			continue
		}

		oldVal, oldExists := beforeMap[key]
		newVal, newExists := afterMap[key]

		if !oldExists && newExists {
			// 新增字段
			diffs = append(diffs, audited.FieldChange{Field: key, Old: nil, New: jsoncodec.NormalizeNumbers(newVal)})
		} else if oldExists && !newExists {
			// 删除字段
			diffs = append(diffs, audited.FieldChange{Field: key, Old: jsoncodec.NormalizeNumbers(oldVal), New: nil})
		} else if !reflect.DeepEqual(oldVal, newVal) {
			// 变更字段
			diffs = append(diffs, audited.FieldChange{
				Field: key,
				Old:   jsoncodec.NormalizeNumbers(oldVal),
				New:   jsoncodec.NormalizeNumbers(newVal),
			})
		}
	}

	if len(diffs) == 0 {
		return nil
	}

	result, err := json.Marshal(diffs)
	if err != nil {
		return nil
	}
	return result
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
	// JSON tag 可能与 Go 字段名不同，同时支持两种命名
	"ID":        {},
	"Version":   {},
	"CreatedAt": {},
	"CreatedBy": {},
	"UpdatedAt": {},
	"UpdatedBy": {},
	"DeletedAt": {},
	"DeletedBy": {},
}

// isAuditIgnoredField 判断审计Ignored字段。
func isAuditIgnoredField(field string) bool {
	_, ignored := auditIgnoredFields[field]
	return ignored
}
