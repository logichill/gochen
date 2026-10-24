package audited

import (
	"context"
	"time"

	"gochen/app/internal/depcheck"
	"gochen/app/internal/writeflow"
	"gochen/contextx"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

const updateAllBeforeSnapshotChunkSize = 200

// CreateAll 批量创建实体，并在同一事务中保存审计记录。
func (s *Application[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(entities), s.Config().MaxBatchSize); err != nil {
		return err
	}
	for i, entity := range entities {
		if depcheck.IsNil(entity) {
			return errors.NewCode(errors.InvalidInput, "entity cannot be nil").WithContext("index", i)
		}
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}
	hooks := s.Application.Hooks()
	return writeflow.New[[]T](s.txRepo).
		Action(s.batchCreateAll()).
		Before(writeflow.Each(hooks.Create.Before)).
		Validate(writeflow.EachPure(s.Application.Validate)).
		After(writeflow.Each(hooks.Create.After)).
		After(s.afterCreateAllAudit(entities)).
		PostCommit(writeflow.EachAll(hooks.Create.PostCommit)).
		OutsideTx().
		Run(ctx, entities)
}

// UpdateAll 批量更新实体，并在同一事务中保存审计差异。
func (s *Application[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if len(entities) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(entities), s.Config().MaxBatchSize); err != nil {
		return err
	}
	for i, entity := range entities {
		if depcheck.IsNil(entity) {
			return errors.NewCode(errors.InvalidInput, "entity cannot be nil").WithContext("index", i)
		}
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	ids := uniqueEntityIDs(entities)
	expectedVersions, err := versionsByEntityID(entities)
	if err != nil {
		return err
	}

	hooks := s.Application.Hooks()
	audit := &updateAllAudit[T, ID]{app: s, entities: entities, ids: ids, expectedVersions: expectedVersions}
	return writeflow.New[[]T](s.txRepo).
		Action(audit.write).
		Before(writeflow.Each(hooks.Update.Before)).
		Validate(writeflow.EachPure(s.Application.Validate)).
		After(writeflow.Each(hooks.Update.After)).
		After(audit.saveDiffs).
		PostCommit(writeflow.EachAll(hooks.Update.PostCommit)).
		OutsideTx().
		Run(ctx, entities)
}

// DeleteAll 批量软删除，为每个实体写入 DELETE 审计记录。
func (s *Application[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if len(ids) == 0 {
		return nil
	}
	if err := writeflow.ValidateBatchSize(len(ids), s.Config().MaxBatchSize); err != nil {
		return err
	}
	ids = dedupIDs(ids)
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}

	hooks := s.Application.Hooks()
	return writeflow.New[[]ID](s.txRepo).
		Action(s.batchSoftDeleteAll(ids)).
		Before(writeflow.Each(hooks.Delete.Before)).
		After(writeflow.Each(hooks.Delete.After)).
		After(s.afterDeleteAllAudit(ids)).
		PostCommit(writeflow.EachAll(hooks.Delete.PostCommit)).
		OutsideTx().
		Run(ctx, ids)
}

// batchCreateAll 解析批量创建的核心动作：优先批量接口，否则逐条写入。
func (s *Application[T, ID]) batchCreateAll() func(context.Context, []T) error {
	repo := s.Repository()
	if batchRepo, ok := any(repo).(interface {
		CreateAll(ctx context.Context, entities []T) error
	}); ok {
		return batchRepo.CreateAll
	}
	return func(ctx context.Context, entities []T) error {
		for _, entity := range entities {
			if err := repo.Create(ctx, entity); err != nil {
				return err
			}
		}
		return nil
	}
}

func (s *Application[T, ID]) afterCreateAllAudit(entities []T) func(context.Context, []T) error {
	return func(ctx context.Context, _ []T) error {
		by := contextx.Operator(ctx)
		if by == "" {
			return errors.NewCode(errors.Internal, "CreateAll: operator not found in tx context (caller bug)")
		}
		now := time.Now()
		records := make([]domaudited.AuditRecord, 0, len(entities))
		for _, entity := range entities {
			snapshot, err := s.marshalAuditSnapshot(entity)
			if err != nil {
				return err
			}
			records = append(records, s.buildAuditRecord(by, now, entity.GetID(), domaudited.AuditOpCreate, snapshot))
		}
		return s.saveAuditRecords(s.auditContext(ctx), records)
	}
}

// updateAllAudit 承载批量更新流程的跨阶段状态：事务内装载的写入前快照。
type updateAllAudit[T domain.IEntity[ID], ID comparable] struct {
	app              *Application[T, ID]
	entities         []T
	ids              []ID
	expectedVersions map[ID]uint64
	beforeByID       map[ID]T
}

func (u *updateAllAudit[T, ID]) write(ctx context.Context, _ []T) error {
	beforeByID, err := u.app.collectUpdateBeforeSnapshots(ctx, u.ids)
	if err != nil {
		return err
	}
	u.beforeByID = beforeByID
	if err := ensureBatchVersionsMatch(beforeByID, u.expectedVersions, u.entities); err != nil {
		return err
	}
	for _, entity := range u.entities {
		if err := u.app.restoreManagedUpdateFields(beforeByID[entity.GetID()], entity); err != nil {
			return err
		}
	}
	repo := u.app.Repository()
	if batchRepo, ok := any(repo).(interface {
		UpdateAll(ctx context.Context, entities []T) error
	}); ok {
		return batchRepo.UpdateAll(ctx, u.entities)
	}
	for _, entity := range u.entities {
		if err := repo.Update(ctx, entity); err != nil {
			return err
		}
	}
	return nil
}

func (u *updateAllAudit[T, ID]) saveDiffs(ctx context.Context, _ []T) error {
	by := contextx.Operator(ctx)
	if by == "" {
		return errors.NewCode(errors.Internal, "UpdateAll: operator not found in tx context (caller bug)")
	}
	now := time.Now()
	records := make([]domaudited.AuditRecord, 0, len(u.entities))
	for _, entity := range u.entities {
		diff, err := u.app.computeAuditDiff(u.beforeByID[entity.GetID()], entity)
		if err != nil {
			return err
		}
		records = append(records, u.app.buildAuditRecord(by, now, entity.GetID(), domaudited.AuditOpUpdate, diff))
	}
	return u.app.saveAuditRecords(u.app.auditContext(ctx), records)
}

func (s *Application[T, ID]) batchSoftDeleteAll(ids []ID) func(context.Context, []ID) error {
	repo := s.Repository()
	return func(ctx context.Context, _ []ID) error {
		for _, id := range ids {
			entity, err := s.softDeleteEntity(ctx, id)
			if err != nil {
				return err
			}
			if err := repo.Update(ctx, entity); err != nil {
				return err
			}
		}
		return nil
	}
}

func (s *Application[T, ID]) afterDeleteAllAudit(ids []ID) func(context.Context, []ID) error {
	return func(ctx context.Context, _ []ID) error {
		by := contextx.Operator(ctx)
		if by == "" {
			return errors.NewCode(errors.Internal, "DeleteAll: operator not found in tx context (caller bug)")
		}
		now := time.Now()
		records := make([]domaudited.AuditRecord, 0, len(ids))
		for _, id := range ids {
			records = append(records, s.buildAuditRecord(by, now, id, domaudited.AuditOpDelete, nil))
		}
		return s.saveAuditRecords(s.auditContext(ctx), records)
	}
}

func (s *Application[T, ID]) collectUpdateBeforeSnapshots(ctx context.Context, ids []ID) (map[ID]T, error) {
	beforeByID := make(map[ID]T, len(ids))

	if listRepo, ok := any(s.Repository()).(interface {
		ListByIds(ctx context.Context, ids []ID) ([]T, error)
	}); ok && listRepo != nil {
		for start := 0; start < len(ids); start += updateAllBeforeSnapshotChunkSize {
			end := start + updateAllBeforeSnapshotChunkSize
			if end > len(ids) {
				end = len(ids)
			}
			records, err := listRepo.ListByIds(ctx, ids[start:end])
			if err != nil {
				return nil, err
			}
			for _, before := range records {
				beforeByID[before.GetID()] = before
			}
		}
		for _, id := range ids {
			if _, ok := beforeByID[id]; ok {
				continue
			}
			before, err := s.Repository().Get(ctx, id)
			if err != nil {
				return nil, err
			}
			beforeByID[id] = before
		}
		return beforeByID, nil
	}

	for _, id := range ids {
		before, err := s.Repository().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		beforeByID[id] = before
	}
	return beforeByID, nil
}

func versionsByEntityID[T interface {
	GetID() ID
	GetVersion() uint64
}, ID comparable](entities []T) (map[ID]uint64, error) {
	versions := make(map[ID]uint64, len(entities))
	for _, entity := range entities {
		if _, ok := versions[entity.GetID()]; ok {
			return nil, errors.NewCode(errors.InvalidInput, "duplicate entity id in batch update").
				WithContext("id", entity.GetID())
		}
		versions[entity.GetID()] = entity.GetVersion()
	}
	return versions, nil
}

func ensureBatchVersionsMatch[T interface {
	GetID() ID
	GetVersion() uint64
}, ID comparable](beforeByID map[ID]T, expectedVersions map[ID]uint64, entities []T) error {
	for _, entity := range entities {
		before, ok := beforeByID[entity.GetID()]
		if !ok {
			return errors.NewCode(errors.NotFound, "record not found").
				WithContext("id", entity.GetID())
		}
		want, ok := expectedVersions[entity.GetID()]
		if !ok {
			return errors.NewCode(errors.Internal, "missing expected version").
				WithContext("id", entity.GetID())
		}
		if got := before.GetVersion(); want != got {
			return errors.NewCode(errors.Concurrency, "concurrency conflict").
				WithContext("id", entity.GetID()).
				WithContext("expected_version", want).
				WithContext("actual_version", got)
		}
	}
	return nil
}

func uniqueEntityIDs[T interface{ GetID() ID }, ID comparable](entities []T) []ID {
	ids := make([]ID, 0, len(entities))
	for _, entity := range entities {
		ids = append(ids, entity.GetID())
	}
	return dedupIDs(ids)
}

// dedupIDs 保持首次出现顺序去重，使批量软删对重复 ID 幂等（与 repo 层 IN 语义对齐）。
func dedupIDs[ID comparable](ids []ID) []ID {
	return writeflow.DedupIDs(ids)
}
