package eventsourced

import (
	"context"
	"gochen/domain/spec"

	"gochen/app"
	"gochen/app/eventsourced/eventstore"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/internal/depcheck"
	"gochen/validate"
)

// Options 是事件溯源应用的构造期配置；构造完成后全部冻结。
//
// Actions 只声明自定义动作；标准动作 read / create / update 由构造函数
// 自动加入，配置 History 时另加 audit_read。
type Options[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	Resource          string
	Authorizer        auth.IAuthorizer
	Actions           []auth.Action
	ActionPermissions map[string]string
	Validator         validate.IValidator
	Limits            execution.Limits
	Contract          app.ResourceContract

	// ConcurrencyRetry 配置保存阶段并发冲突时的自动重试；重试会重新加载
	// 聚合并重新执行命令函数，因此命令必须可重入。
	ConcurrencyRetry *RetryConfig

	// IsConcurrencyError 自定义并发冲突判断；默认按 errors.Concurrency 判断。
	IsConcurrencyError IsConcurrencyError

	// History 提供事件历史读取能力；配置后提供 AuditTrail 并声明 audit_read。
	History eventstore.IEventHistoryStore[ID]

	// HistoryMapper 将事件映射为业务可读历史条目；为 nil 时使用默认映射。
	HistoryMapper eventstore.EventHistoryMapper[ID]
}

// Application 是事件溯源业务侧的唯一入口：授权、匹配、命令后校验与审计
// 元数据由同一对象统一编排。
type Application[T deventsourced.IEventSourcedAggregate[ID], ID comparable] struct {
	repo        deventsourced.IEventSourcedRepository[T, ID]
	resource    string
	authorizer  auth.IAuthorizer
	vocabulary  auth.Vocabulary
	descriptor  auth.Descriptor
	actions     map[string]auth.Action
	validator   validate.IValidator
	limits      execution.Limits
	metadata    *meta.Metadata
	interpreter *spec.Interpreter
	execution   app.ExecutionDescriptor

	retryConfig        *RetryConfig
	isConcurrencyError IsConcurrencyError

	history       eventstore.IEventHistoryStore[ID]
	historyMapper eventstore.EventHistoryMapper[ID]
}

// NewApplication 创建事件溯源应用。
func NewApplication[T deventsourced.IEventSourcedAggregate[ID], ID comparable](
	repo deventsourced.IEventSourcedRepository[T, ID],
	options Options[T, ID],
) (*Application[T, ID], error) {
	if depcheck.IsNil(repo) {
		return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
	}
	if depcheck.IsNil(options.Authorizer) {
		return nil, errors.NewCode(errors.InvalidInput, "authorizer cannot be nil")
	}
	metadata, err := meta.Describe[T]()
	if err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "aggregate metadata is invalid")
	}
	if err := execution.ValidateIDType[ID](); err != nil {
		return nil, err
	}
	withAudit := !depcheck.IsNil(options.History)
	defaults, err := standardActions(options.Resource, actionPermissions(options.ActionPermissions), withAudit)
	if err != nil {
		return nil, err
	}
	descriptor, actions, err := execution.BuildDescriptor(options.Resource, defaults, options.Actions)
	if err != nil {
		return nil, err
	}
	vocabulary := options.Authorizer.Vocabulary().Copy()
	effective, err := app.BindContract(metadata, options.Contract)
	if err != nil {
		return nil, err
	}
	if err := effective.ValidateVocabulary(vocabulary); err != nil {
		return nil, err
	}
	application := &Application[T, ID]{
		repo:               repo,
		resource:           descriptor.Resource,
		authorizer:         options.Authorizer,
		vocabulary:         vocabulary,
		descriptor:         descriptor,
		actions:            actions,
		validator:          options.Validator,
		limits:             options.Limits.Normalize(),
		metadata:           metadata,
		interpreter:        spec.NewInterpreter(),
		execution:          effective,
		retryConfig:        normalizeRetryConfig(options.ConcurrencyRetry),
		isConcurrencyError: DefaultIsConcurrencyError,
		history:            options.History,
		historyMapper:      options.HistoryMapper,
	}
	if options.IsConcurrencyError != nil {
		application.isConcurrencyError = options.IsConcurrencyError
	}
	return application, nil
}

// Get 回放聚合，并按 read 规约校验当前状态。
func (a *Application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	spec, _, err := a.authorizeKind(ctx, ActionRead, []ID{id}, auth.KindRead)
	if err != nil {
		return zero, err
	}
	aggregate, err := a.repo.Get(ctx, id)
	if err != nil {
		return zero, err
	}
	if err := a.matchAggregate(spec, aggregate, errors.NotFound); err != nil {
		return zero, err
	}
	return aggregate, nil
}

// Execute 修改已有聚合：授权 → 回放并匹配 → 命令 → 赋值核对 → 校验 → 匹配写规约 → 保存。
//
// 保存阶段并发冲突时按 ConcurrencyRetry 重试；每次重试都会重新回放并重新执行命令。
func (a *Application[T, ID]) Execute(
	ctx context.Context,
	action string,
	id ID,
	fn func(ctx context.Context, aggregate T) error,
	opts ...ExecOption,
) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	if fn == nil {
		return zero, errors.NewCode(errors.InvalidInput, "command function cannot be nil")
	}
	spec, decision, err := a.authorizeKind(ctx, action, []ID{id}, auth.KindUpdate)
	if err != nil {
		return zero, err
	}
	if err := a.execution.ValidateWriteSpec(spec, false); err != nil {
		return zero, err
	}
	runCtx, err := a.commandContext(ctx, action, opts)
	if err != nil {
		return zero, err
	}
	result, err := commandflowRun(runCtx, a.retryConfig, a.isConcurrencyError, func(attemptCtx context.Context) (T, error) {
		return a.executeAttempt(attemptCtx, id, spec, decision.Assign, fn)
	})
	if err != nil && !contextx.IsAfterCommitError(err) {
		return zero, err
	}
	return result, err
}

// ExecuteNew 新建聚合：授权 → 命令 → 核对新聚合 → 赋值核对 → 校验 → 匹配新建规约 → 保存。
//
// 新建不自动重试：乐观锁冲突说明目标聚合已存在，重跑命令不会改变结果。
func (a *Application[T, ID]) ExecuteNew(
	ctx context.Context,
	action string,
	fn func(ctx context.Context) (T, error),
	opts ...ExecOption,
) (T, error) {
	var zero T
	if err := checkContext(ctx); err != nil {
		return zero, err
	}
	if fn == nil {
		return zero, errors.NewCode(errors.InvalidInput, "command function cannot be nil")
	}
	spec, decision, err := a.authorizeKind(ctx, action, nil, auth.KindCreate)
	if err != nil {
		return zero, err
	}
	if err := a.execution.ValidateWriteSpec(spec, true); err != nil {
		return zero, err
	}
	for _, assignment := range decision.Assign {
		if !a.execution.CanAssign(assignment.Dimension) {
			return zero, errors.NewCode(errors.Internal, "create assignment lacks resource capability").
				WithContext("dimension", string(assignment.Dimension))
		}
	}
	runCtx, err := a.commandContext(ctx, action, opts)
	if err != nil {
		return zero, err
	}
	aggregate, err := fn(runCtx)
	if err != nil {
		return zero, err
	}
	if depcheck.IsNil(aggregate) {
		return zero, errors.NewCode(errors.InvalidInput, "command produced a nil aggregate")
	}
	if aggregate.GetExpectedVersion() != 0 {
		aggregate.ClearUncommittedEvents()
		return zero, errors.NewCode(errors.InvalidInput, "create requires a new aggregate").
			WithContext("aggregate_id", execution.FormatID(aggregate.GetID()))
	}
	if err := a.validateAfterCommand(aggregate, decision.Assign); err != nil {
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	if err := a.matchAggregate(spec, aggregate, errors.Forbidden); err != nil {
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	if err := a.repo.Save(runCtx, aggregate); err != nil {
		if contextx.IsAfterCommitError(err) {
			return aggregate, err
		}
		aggregate.ClearUncommittedEvents()
		return zero, err
	}
	return aggregate, nil
}

// AuditTrail 读取聚合的事件历史视图。
//
// 读取前先回放并按 audit_read 规约校验当前状态；事件元数据不携带边界。
func (a *Application[T, ID]) AuditTrail(
	ctx context.Context,
	id ID,
	offset int,
	limit int,
) ([]*eventstore.EventHistoryEntry[ID], error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if depcheck.IsNil(a.history) {
		return nil, errors.NewCode(errors.Unsupported, "event history is not configured")
	}
	spec, _, err := a.authorizeKind(ctx, ActionAuditRead, []ID{id}, auth.KindRead)
	if err != nil {
		return nil, err
	}
	if err := execution.ValidateAuditSpec(spec, a.metadata); err != nil {
		return nil, err
	}
	aggregate, err := a.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.matchAggregate(spec, aggregate, errors.NotFound); err != nil {
		return nil, err
	}
	offset, limit = a.normalizePage(offset, limit)
	return eventstore.LoadEventHistoryEntries(ctx, a.history, aggregate.GetAggregateType(), id, offset, limit, a.historyMapper)
}

// Descriptor 返回构造期冻结的资源动作清单。
func (a *Application[T, ID]) Descriptor() auth.Descriptor {
	out := a.descriptor
	out.Actions = append([]auth.Action(nil), a.descriptor.Actions...)
	return out
}

// MaxPageSize 返回事件历史单页上限。
func (a *Application[T, ID]) MaxPageSize() int { return a.limits.MaxPageSize }

// UsesAllowAuthorizer 报告当前应用是否使用一律放行的授权器（D17 启动清单）。
func (a *Application[T, ID]) UsesAllowAuthorizer() bool { return auth.IsAllow(a.authorizer) }

func (a *Application[T, ID]) authorizeKind(ctx context.Context, action string, ids []ID, kinds ...auth.Kind) (spec.Spec, auth.Decision, error) {
	canonical := make([]string, 0, len(ids))
	for _, id := range ids {
		canonical = append(canonical, execution.FormatID(id))
	}
	return execution.AuthorizeKind(ctx, a.resource, a.authorizer, a.descriptor, a.actions, a.vocabulary, a.metadata, action, canonical, kinds...)
}

func (a *Application[T, ID]) matchAggregate(spec spec.Spec, aggregate T, code errors.ErrorCode) error {
	matched, err := a.interpreter.Match(spec, aggregate)
	if err != nil {
		return err
	}
	if matched {
		return nil
	}
	return errors.NewCode(code, "aggregate does not match the authorization spec").
		WithContext("resource", a.resource).
		WithContext("aggregate_type", aggregate.GetAggregateType()).
		WithContext("aggregate_id", execution.FormatID(aggregate.GetID()))
}

func (a *Application[T, ID]) validateAfterCommand(aggregate T, assignments []auth.Assignment) error {
	if err := execution.ValidateAssignments(aggregate, assignments, a.metadata); err != nil {
		return err
	}
	if validatable, ok := any(aggregate).(domain.IValidatable); ok {
		if err := validatable.Validate(); err != nil {
			return err
		}
	}
	if a.validator != nil {
		return a.validator.Validate(aggregate)
	}
	return nil
}

func (a *Application[T, ID]) normalizePage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = a.limits.MaxPageSize
	}
	if limit > a.limits.MaxPageSize {
		limit = a.limits.MaxPageSize
	}
	return offset, limit
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return nil
}
