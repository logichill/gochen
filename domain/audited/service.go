// Package audited 提供带审计能力的服务接口与默认实现。
// 在 CRUD 服务能力基础上扩展审计轨迹与软删/恢复逻辑，
// 用于承载“CRUD + 审计/软删除”场景的推荐入口：`domain/audited`。
package audited

import (
	"context"
	"fmt"
	"time"

	"gochen/domain/crud"
)

// IAuditedService 面向带审计/软删场景的服务接口
// 在 CRUD 服务基础上引入审计追踪、软删/恢复等能力
type IAuditedService[T IAuditedEntity[ID], ID comparable] interface {
	crud.IService[T, ID]

	GetAuditTrail(ctx context.Context, id ID) ([]AuditRecord, error)
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	Restore(ctx context.Context, id ID, by string) error
	SoftDelete(ctx context.Context, id ID, by string) error
	PermanentDelete(ctx context.Context, id ID) error

	AuditedRepository() IAuditedRepository[T, ID]
}

// AuditedService 基于 IAuditedRepository 的默认实现
type AuditedService[T IAuditedEntity[ID], ID comparable] struct {
	*crud.CRUDService[T, ID]
	repo       IAuditedRepository[T, ID]
	auditStore IAuditStore
}

// NewAuditedService 创建带审计能力的通用服务实现
func NewAuditedService[T IAuditedEntity[ID], ID comparable](r IAuditedRepository[T, ID], store IAuditStore) *AuditedService[T, ID] {
	return &AuditedService[T, ID]{
		CRUDService: crud.NewCRUDService[T, ID](r),
		repo:       r,
		auditStore: store,
	}
}

func (s *AuditedService[T, ID]) GetAuditTrail(ctx context.Context, id ID) ([]AuditRecord, error) {
	if s.auditStore == nil {
		return nil, nil
	}
	entityID := fmt.Sprint(id)
	// 默认返回前 100 条审计记录，调用方可在需要时自行扩展为带分页的接口。
	return s.auditStore.ListAuditRecordsByEntity(ctx, entityID, 0, 100)
}

func (s *AuditedService[T, ID]) ListDeleted(ctx context.Context, o, l int) ([]T, error) {
	// 优先使用可查询仓储接口
	if qr, ok := any(s.repo).(crud.IQueryableRepository[T, ID]); ok {
		opts := crud.QueryOptions{
			Offset:        o,
			Limit:         l,
			IncludeDeleted: true,
		}
		entities, err := qr.Query(ctx, opts)
		if err != nil {
			return nil, err
		}
		// 保险起见，再按照 IsDeleted 过滤一遍
		result := make([]T, 0, len(entities))
		for _, e := range entities {
			if e.IsDeleted() {
				result = append(result, e)
			}
		}
		return result, nil
	}

	// 退化实现：拉取较大窗口后在内存中过滤已软删实体
	all, err := s.repo.List(ctx, 0, 1<<20)
	if err != nil {
		return nil, err
	}
	deleted := make([]T, 0)
	for _, e := range all {
		if e.IsDeleted() {
			deleted = append(deleted, e)
		}
	}
	if o >= len(deleted) {
		return []T{}, nil
	}
	end := o + l
	if end > len(deleted) {
		end = len(deleted)
	}
	return deleted[o:end], nil
}

func (s *AuditedService[T, ID]) Restore(ctx context.Context, id ID, by string) error {
	e, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := e.Restore(); err != nil {
		return err
	}
	now := time.Now()
	e.SetUpdatedInfo(by, now)
	if err := s.repo.Update(ctx, e); err != nil {
		return err
	}
	return s.saveAudit(ctx, id, "RESTORE", by, nil)
}

func (s *AuditedService[T, ID]) SoftDelete(ctx context.Context, id ID, by string) error {
	e, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now()
	if err := e.SoftDelete(by, now); err != nil {
		return err
	}
	e.SetUpdatedInfo(by, now)
	if err := s.repo.Update(ctx, e); err != nil {
		return err
	}
	return s.saveAudit(ctx, id, "DELETE", by, nil)
}

func (s *AuditedService[T, ID]) PermanentDelete(ctx context.Context, id ID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	// 操作人缺省为 system，调用方可在上层扩展更丰富的上下文。
	return s.saveAudit(ctx, id, "DELETE_HARD", "system", nil)
}

// Create 覆盖 CRUDService 的 Create，实现自动记录审计日志。
func (s *AuditedService[T, ID]) Create(ctx context.Context, e T) error {
	if err := s.CRUDService.Create(ctx, e); err != nil {
		return err
	}
	return s.saveAudit(ctx, e.GetID(), "CREATE", "", nil)
}

// Update 覆盖 CRUDService 的 Update，实现自动记录审计日志。
func (s *AuditedService[T, ID]) Update(ctx context.Context, e T) error {
	if err := s.CRUDService.Update(ctx, e); err != nil {
		return err
	}
	return s.saveAudit(ctx, e.GetID(), "UPDATE", "", nil)
}

// Delete 覆盖 CRUDService 的 Delete，实现自动记录硬删审计日志。
func (s *AuditedService[T, ID]) Delete(ctx context.Context, id ID) error {
	if err := s.CRUDService.Delete(ctx, id); err != nil {
		return err
	}
	return s.saveAudit(ctx, id, "DELETE_HARD", "system", nil)
}

func (s *AuditedService[T, ID]) AuditedRepository() IAuditedRepository[T, ID] { return s.repo }

// saveAudit 构造并保存一条审计记录（如果配置了审计存储）。
func (s *AuditedService[T, ID]) saveAudit(ctx context.Context, id ID, op, by string, changes map[string]any) error {
	if s.auditStore == nil {
		return nil
	}
	now := time.Now()
	rec := AuditRecord{
		ID:        now.UnixNano(),
		EntityID:  fmt.Sprint(id),
		Operation: op,
		Operator:  by,
		Timestamp: now,
		Changes:   changes,
	}
	return s.auditStore.SaveAuditRecord(ctx, rec)
}
