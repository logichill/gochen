package app

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"gochen/auth"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

type commandSnapshot struct {
	protected map[string]any
	version   uint64
}

// ILockedReader 在当前事务内按条件锁定并读取一个资源。
type ILockedReader[T domain.IEntity[ID], ID comparable] interface {
	FindForUpdate(context.Context, ID, spec.Spec) (T, error)
}

// CommandRepository 在一次命令内自动传递授权条件，并保护已读实体的归属与版本。
// 业务函数仅接收该能力，实际仓储由组合根绑定到 CommandResource。
type CommandRepository[T domain.IEntity[ID], ID comparable] struct {
	repo   domcrud.IRepository[T, ID]
	grant  *CommandGrant
	mu     sync.Mutex
	loaded map[ID]commandSnapshot
}

// NewCommandRepository 仅允许绑定声明中的同一仓储实例。
func NewCommandRepository[T domain.IEntity[ID], ID comparable](repo domcrud.IRepository[T, ID], grant *CommandGrant) (*CommandRepository[T, ID], error) {
	if depcheck.IsNil(repo) || grant == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command repository requires a resource grant")
	}
	provider, ok := any(repo).(IExecutionDescriptorProvider)
	if !ok || !reflect.ValueOf(provider).Comparable() || any(provider) != any(grant.repository) {
		return nil, errors.NewCode(errors.InvalidInput, "repository is outside the command resource declaration")
	}
	return &CommandRepository[T, ID]{repo: repo, grant: grant, loaded: map[ID]commandSnapshot{}}, nil
}

// Get 在授权条件内读取并冻结归属与版本，用于后续写保护。
func (r *CommandRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := r.grant.check(ctx); err != nil {
		return zero, err
	}
	if err := r.checkID(id); err != nil {
		return zero, err
	}
	entity, err := r.repo.Get(ctx, id, r.grant.decision.Spec)
	if err != nil {
		return zero, err
	}
	return r.track(entity)
}

// FindForUpdate 通过声明中的仓储执行条件锁定读取，冻结后续写保护基线。
func (r *CommandRepository[T, ID]) FindForUpdate(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := r.grant.check(ctx); err != nil {
		return zero, err
	}
	if err := r.checkID(id); err != nil {
		return zero, err
	}
	reader, ok := r.repo.(ILockedReader[T, ID])
	if !ok {
		return zero, errors.NewCode(errors.Unsupported, "command repository does not support locked reads")
	}
	entity, err := reader.FindForUpdate(ctx, id, r.grant.decision.Spec)
	if err != nil {
		return zero, err
	}
	return r.track(entity)
}

func (r *CommandRepository[T, ID]) checkID(id ID) error {
	if len(r.grant.request.IDs) > 0 {
		found := false
		for _, declared := range r.grant.request.IDs {
			found = found || declared == fmt.Sprint(id)
		}
		if !found {
			return errors.NewCode(errors.Forbidden, "resource ID is outside the command intent")
		}
	}
	return nil
}

// FromFact 复用注册提供方实际读取的可信实体；客户端值保持为输入意图。
func (r *CommandRepository[T, ID]) FromFact(ctx context.Context, fact *auth.Fact[T]) (T, error) {
	var zero T
	if err := r.grant.check(ctx); err != nil {
		return zero, err
	}
	request, err := r.grant.Request()
	if err != nil {
		return zero, err
	}
	entity, err := fact.Get(ctx, request)
	if err != nil {
		return zero, err
	}
	return r.track(entity)
}

func (r *CommandRepository[T, ID]) track(entity T) (T, error) {
	var zero T
	if depcheck.IsNil(entity) {
		return zero, errors.NewCode(errors.NotFound, "command resource was not found")
	}
	if err := r.checkID(entity.GetID()); err != nil {
		return zero, err
	}
	if allowed, err := spec.NewInterpreter().Match(r.grant.decision.Spec, entity); err != nil {
		return zero, err
	} else if !allowed {
		return zero, errors.NewCode(errors.Forbidden, "trusted resource is outside the authorized scope")
	}
	protected, err := r.grant.execution.CaptureProtected(entity)
	if err != nil {
		return zero, err
	}
	protected, err = meta.Clone(protected)
	if err != nil {
		return zero, err
	}
	r.mu.Lock()
	r.loaded[entity.GetID()] = commandSnapshot{protected: protected, version: entity.GetVersion()}
	r.mu.Unlock()
	return meta.Clone(entity)
}

// Create 自动应用服务端赋值，按创建规约交给仓储执行。
func (r *CommandRepository[T, ID]) Create(ctx context.Context, entity T) error {
	if err := r.grant.check(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "command entity is required")
	}
	if r.grant.action.Kind != auth.KindCreate {
		return errors.NewCode(errors.Forbidden, "command action cannot create this resource")
	}
	if err := r.grant.execution.ValidateWriteSpec(r.grant.decision.Spec, true); err != nil {
		return err
	}
	if err := ApplyAssignments(entity, r.grant.decision.Assign, r.grant.execution); err != nil {
		return err
	}
	return r.repo.Create(ctx, entity, r.grant.decision.Spec)
}

// Update 核对本次可信读取的归属与调用方版本，再执行原子条件写入。
func (r *CommandRepository[T, ID]) Update(ctx context.Context, entity T) error {
	if err := r.grant.check(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "command entity is required")
	}
	if r.grant.action.Kind != auth.KindUpdate {
		return errors.NewCode(errors.Forbidden, "command action cannot update this resource")
	}
	if err := r.checkWrite(entity); err != nil {
		return err
	}
	if err := r.repo.Update(ctx, entity, r.grant.decision.Spec); err != nil {
		return err
	}
	_, err := r.track(entity)
	return err
}

// Delete 通过本次已读取的实体版本执行条件删除。
func (r *CommandRepository[T, ID]) Delete(ctx context.Context, entity T) error {
	if err := r.grant.check(ctx); err != nil {
		return err
	}
	if depcheck.IsNil(entity) {
		return errors.NewCode(errors.InvalidInput, "command entity is required")
	}
	if r.grant.action.Kind != auth.KindDelete {
		return errors.NewCode(errors.Forbidden, "command action cannot delete this resource")
	}
	if err := r.checkWrite(entity); err != nil {
		return err
	}
	return r.repo.Delete(ctx, entity, r.grant.decision.Spec)
}

func (r *CommandRepository[T, ID]) checkWrite(entity T) error {
	if err := r.grant.execution.ValidateWriteSpec(r.grant.decision.Spec, false); err != nil {
		return err
	}
	r.mu.Lock()
	snapshot, loaded := r.loaded[entity.GetID()]
	r.mu.Unlock()
	if !loaded {
		return errors.NewCode(errors.Forbidden, "write requires a trusted read in this command")
	}
	if entity.GetVersion() != snapshot.version {
		return errors.NewCode(errors.Concurrency, "command entity version changed")
	}
	return r.grant.execution.CheckProtected(snapshot.protected, entity)
}
