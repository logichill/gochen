package audited

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gochen/contextx"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// AuditProjector 在生成审计快照前投影实体，用于脱敏或筛选字段。
// 返回原实体或不配置投影函数时保留完整实体。
type AuditProjector[T any] func(entity T) (any, error)

// WithAuditProjector 注入审计投影函数；用于在审计快照中脱敏或投影实体字段。
// 可在运行期间替换，后续写入使用新投影函数。
func (s *Application[T, ID]) WithAuditProjector(projector AuditProjector[T]) *Application[T, ID] {
	if s == nil || projector == nil {
		return s
	}
	s.projectorMu.Lock()
	s.auditProjector = projector
	s.projectorMu.Unlock()
	return s
}

// AuditStore 返回应用使用的审计存储。
//
// 仅供本包内部装配使用；对外能力接口（IAuditOperations）只暴露 HasAuditStore，
// 防止调用方绕过安全控制直接改写审计数据。
func (s *Application[T, ID]) AuditStore() domaudited.IAuditStore { return s.auditStore }

// HasAuditStore 报告底层审计存储是否已就绪。
func (s *Application[T, ID]) HasAuditStore() bool {
	return s != nil && s.auditStore != nil
}

// AuditTrail 返回指定实体的审计记录，支持分页。
func (s *Application[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]domaudited.AuditRecord, error) {
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
	return s.auditStore.ListAuditRecordsByEntity(s.auditContext(ctx), fmt.Sprint(id), offset, limit)
}

func (s *Application[T, ID]) projectForAudit(entity T) (any, error) {
	if s == nil {
		return entity, nil
	}
	s.projectorMu.RLock()
	projector := s.auditProjector
	s.projectorMu.RUnlock()
	if projector == nil {
		return entity, nil
	}
	return projector(entity)
}

func (s *Application[T, ID]) auditContext(ctx context.Context) context.Context {
	if s == nil || s.resourceKind == "" {
		return ctx
	}
	return domaudited.WithAuditResourceKind(ctx, s.resourceKind)
}

// saveAuditRecords 保存审计Records。
func (s *Application[T, ID]) saveAuditRecords(ctx context.Context, records []domaudited.AuditRecord) error {
	if len(records) == 0 {
		return nil
	}
	if s.auditStore == nil {
		return errors.NewCode(errors.Internal, "auditStore is nil")
	}
	if batch, ok := any(s.auditStore).(domaudited.IBatchAuditStore); ok && batch != nil {
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
func (s *Application[T, ID]) saveAudit(ctx context.Context, id ID, op domaudited.AuditOperation, changes json.RawMessage) error {
	by := contextx.Operator(ctx)
	if by == "" {
		// 防御性断言：调用方应已校验并注入 operator；若仍为空则为调用方 bug。
		return errors.NewCode(errors.Internal, "saveAudit: operator not found in context (caller bug)")
	}

	rec := s.buildAuditRecord(by, time.Now(), id, op, changes)
	return s.saveAuditRecords(s.auditContext(ctx), []domaudited.AuditRecord{rec})
}

func (s *Application[T, ID]) computeAuditDiff(before, after T) (json.RawMessage, error) {
	beforeTarget, err := s.projectForAudit(before)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "audit projection failed for before snapshot")
	}
	afterTarget, err := s.projectForAudit(after)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "audit projection failed for after snapshot")
	}
	return computeEntityDiff(beforeTarget, afterTarget)
}

func (s *Application[T, ID]) marshalAuditSnapshot(entity T) (json.RawMessage, error) {
	target, err := s.projectForAudit(entity)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "audit projection failed")
	}
	return marshalAuditSnapshotResult(target)
}

func requireAuditOperator(ctx context.Context) error {
	if contextx.Operator(ctx) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	return nil
}

func (s *Application[T, ID]) buildAuditRecord(by string, now time.Time, id ID, op domaudited.AuditOperation, changes json.RawMessage) domaudited.AuditRecord {
	return domaudited.AuditRecord{
		ID:           0,
		ResourceKind: s.resourceKind,
		EntityID:     fmt.Sprint(id),
		Operation:    op,
		Operator:     by,
		Timestamp:    now,
		Changes:      changes,
	}
}
