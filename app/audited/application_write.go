package audited

import (
	"context"
	"time"

	"gochen/app/internal/depcheck"
	"gochen/app/internal/writeflow"
	"gochen/domain"
	domaudited "gochen/domain/audited"
	"gochen/errors"
)

// Create 创建记录并写入审计。
func (s *Application[T, ID]) Create(ctx context.Context, entity T) error {
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}
	hooks := s.Application.Hooks()
	return writeflow.New[T](s.txRepo).
		Action(s.Repository().Create).
		Before(hooks.Create.Before).
		Validate(s.Application.Validate).
		After(hooks.Create.After).
		After(s.afterCreateAudit()).
		PostCommit(hooks.Create.PostCommit).
		Run(ctx, entity)
}

// Update 更新记录并写入审计差异；写入前恢复仓储管理的审计字段。
func (s *Application[T, ID]) Update(ctx context.Context, entity T) error {
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	if err := requireAuditOperator(ctx); err != nil {
		return err
	}
	hooks := s.Application.Hooks()
	audit := &updateAudit[T, ID]{app: s, entity: entity}
	return writeflow.New[T](s.txRepo).
		Action(audit.write).
		Before(hooks.Update.Before).
		Validate(s.Application.Validate).
		After(hooks.Update.After).
		After(audit.saveDiff).
		PostCommit(hooks.Update.PostCommit).
		Run(ctx, entity)
}

func (s *Application[T, ID]) afterCreateAudit() func(context.Context, T) error {
	return func(ctx context.Context, entity T) error {
		snapshot, err := s.marshalAuditSnapshot(entity)
		if err != nil {
			return err
		}
		return s.saveAudit(ctx, entity.GetID(), domaudited.AuditOpCreate, snapshot)
	}
}

// updateAudit 承载更新流程的跨阶段状态：事务内装载的写入前快照。
type updateAudit[T domain.IEntity[ID], ID comparable] struct {
	app    *Application[T, ID]
	entity T
	before T
}

func (a *updateAudit[T, ID]) write(ctx context.Context, entity T) error {
	before, err := a.app.Repository().Get(ctx, entity.GetID())
	if err != nil {
		return err
	}
	// 单条更新与批量更新（ensureBatchVersionsMatch）保持同一乐观锁口径：
	// 调用方若显式声明了版本号，必须与事务内读到的当前版本一致，
	// 否则拒绝写入，避免弱隔离仓储下的静默覆盖。
	if entity.GetVersion() != 0 && entity.GetVersion() != before.GetVersion() {
		return errors.NewCode(errors.Conflict, "update version mismatch").
			WithContext("id", entity.GetID()).
			WithContext("expected_version", entity.GetVersion()).
			WithContext("actual_version", before.GetVersion())
	}
	if err := a.app.restoreManagedUpdateFields(before, entity); err != nil {
		return err
	}
	a.before = before
	return a.app.Repository().Update(ctx, entity)
}

func (a *updateAudit[T, ID]) saveDiff(ctx context.Context, entity T) error {
	// 使用流水线传递的最新 payload，而非闭包捕获的构造期实体，
	// 避免 Before/Validate 阶段替换实体时审计快照陈旧。
	diff, err := a.app.computeAuditDiff(a.before, entity)
	if err != nil {
		return err
	}
	return a.app.saveAudit(ctx, entity.GetID(), domaudited.AuditOpUpdate, diff)
}

func (s *Application[T, ID]) asAuditedEntity(entity T) (domaudited.IAuditedEntity[ID], error) {
	ae, ok := any(entity).(domaudited.IAuditedEntity[ID])
	if !ok {
		return nil, errors.NewCode(errors.Internal, "entity does not implement audited.IAuditedEntity")
	}
	return ae, nil
}

// restoreManagedUpdateFields 防止普通更新覆盖由审计仓储管理的字段。
func (s *Application[T, ID]) restoreManagedUpdateFields(before, entity T) error {
	beforeAudited, err := s.asAuditedEntity(before)
	if err != nil {
		return err
	}
	audited, err := s.asAuditedEntity(entity)
	if err != nil {
		return err
	}
	audited.SetCreatedInfo(beforeAudited.GetCreatedBy(), beforeAudited.GetCreatedAt())
	audited.SetUpdatedAt(beforeAudited.GetUpdatedAt())
	if setter, ok := audited.(interface{ SetUpdatedBy(string) }); ok {
		setter.SetUpdatedBy(beforeAudited.GetUpdatedBy())
	} else if audited.GetUpdatedBy() != beforeAudited.GetUpdatedBy() {
		return errors.NewCode(errors.InvalidInput, "updated_by is managed by the audited repository")
	}
	if setter, ok := audited.(interface {
		SetDeletedInfo(*time.Time, *string)
	}); ok {
		setter.SetDeletedInfo(beforeAudited.GetDeletedAt(), beforeAudited.GetDeletedBy())
	} else if !sameOptionalTime(audited.GetDeletedAt(), beforeAudited.GetDeletedAt()) ||
		!sameOptionalString(audited.GetDeletedBy(), beforeAudited.GetDeletedBy()) {
		return errors.NewCode(errors.InvalidInput, "delete fields are managed by the audited application")
	}
	return nil
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
