// Package execution 提供资源写入和可选删除能力共用的私有执行协议。
// 授权、条件写入、事务、归因、审计与钩子保持统一语义。
package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"gochen/app"
	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gochen/app/query"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain"

	"gochen/domain/meta"
	"gochen/errors"
	"gochen/internal/depcheck"
	"gochen/validate"
)

// 标准动作名。自定义动作使用调用方提供的动作名。
const (
	ActionRead        = "read"
	ActionList        = "list"
	ActionCreate      = "create"
	ActionUpdate      = "update"
	ActionDelete      = "delete"
	ActionRestore     = "restore"
	ActionPurge       = "purge"
	ActionAuditRead   = "audit_read"
	ActionListDeleted = "list_deleted"
)

// SystemOperator 是系统身份代为执行时写入审计的执行者标识。
const SystemOperator = "system"

// Stage 定义单个动作的生命周期钩子；After 与 PostCommit 接收独立实体副本。
type Stage[T any] struct {
	Before     func(ctx context.Context, entity T) error
	After      func(ctx context.Context, entity T) error
	PostCommit func(ctx context.Context, entity T) error
}

// Hooks 按动作名键控生命周期钩子；没有隐式回退。
type Hooks[T any] map[string]Stage[T]

// For 返回动作对应的钩子；未配置时为零值。
func (h Hooks[T]) For(action string) Stage[T] {
	if h == nil {
		return Stage[T]{}
	}
	return h[action]
}

// ReadRepository 是执行骨架要求的读端口：按 ID 读取 + 统一查询端口。
type ReadRepository[T domain.IEntity[ID], ID comparable] interface {
	app.IExecutionDescriptorProvider
	Get(ctx context.Context, id ID, s spec.Spec) (T, error)
	query.IQueryableRepository[T]
}

// Options 是构造期配置；所有字段在构造时冻结。
type Options[T domain.IEntity[ID], ID comparable] struct {
	CommandGrant *app.CommandGrant
	Resource     string
	Authorizer   auth.IAuthorizer
	Actions      []auth.Action
	Defaults     []auth.Action
	Hooks        Hooks[T]
	Validator    validate.IValidator
	Limits       app.Limits
	Projector    app.Projector[T]

	Repo  domcrud.IRepository[T, ID]
	Read  ReadRepository[T, ID]
	Tx    app.ITransactionRunner
	Audit app.IAuditRepository

	// 以下端口只在软删/生命周期构造中使用。
	SoftDelete bool
	Restore    domcrud.IRestoreRepository[T, ID]
	Deleted    domcrud.IDeletedQueryRepository[T, ID]
	Purge      domcrud.IPurgeRepository[T, ID]
}

// Core 编排授权、事务、钩子与审计。构造完成后只读，可并发复用。
type Core[T domain.IEntity[ID], ID comparable] struct {
	commandGrant *app.CommandGrant
	resource     string
	authorizer   auth.IAuthorizer
	vocabulary   auth.Vocabulary
	descriptor   auth.Descriptor
	actions      map[string]auth.Action
	hooks        Hooks[T]
	validator    validate.IValidator
	limits       app.Limits
	projector    app.Projector[T]
	metadata     *meta.Metadata
	execution    app.ExecutionDescriptor

	repo  domcrud.IRepository[T, ID]
	read  ReadRepository[T, ID]
	tx    app.ITransactionRunner
	audit app.IAuditRepository

	softDelete bool
	restore    domcrud.IRestoreRepository[T, ID]
	deleted    domcrud.IDeletedQueryRepository[T, ID]
	purge      domcrud.IPurgeRepository[T, ID]
}

// New 构造执行骨架并完成 Descriptor、词汇与数据类型校验。
func New[T domain.IEntity[ID], ID comparable](opts Options[T, ID]) (*Core[T, ID], error) {
	if depcheck.IsNil(opts.Authorizer) {
		return nil, errors.NewCode(errors.InvalidInput, "authorizer cannot be nil")
	}
	if depcheck.IsNil(opts.Read) {
		return nil, errors.NewCode(errors.InvalidInput, "read repository cannot be nil")
	}
	metadata, err := meta.Describe[T]()
	if err != nil {
		return nil, errors.Wrap(err, errors.InvalidInput, "entity metadata is invalid")
	}
	if err := ValidateIDType[ID](); err != nil {
		return nil, err
	}
	descriptor, actions, err := BuildDescriptor(opts.Resource, opts.Defaults, opts.Actions)
	if err != nil {
		return nil, err
	}
	if err := auth.ValidatePlanBinding(opts.Authorizer, descriptor, auth.ResourceAccessData); err != nil {
		return nil, err
	}
	if opts.CommandGrant != nil {
		if err := opts.CommandGrant.BindExecution(opts.Read, opts.Authorizer, descriptor); err != nil {
			return nil, err
		}
	}
	needsWrite := false
	for _, action := range descriptor.Actions {
		switch action.Kind {
		case auth.KindCreate, auth.KindUpdate, auth.KindDelete, auth.KindTransfer:
			needsWrite = true
		}
	}
	if needsWrite {
		if depcheck.IsNil(opts.Repo) {
			return nil, errors.NewCode(errors.InvalidInput, "repository cannot be nil")
		}
		if depcheck.IsNil(opts.Tx) {
			return nil, errors.NewCode(errors.InvalidInput, "repository must implement ITransactional for write actions")
		}
		if opts.CommandGrant != nil {
			provider, ok := opts.Repo.(app.IExecutionDescriptorProvider)
			if !ok {
				return nil, errors.NewCode(errors.InvalidInput, "command write repository must declare its execution descriptor")
			}
			if err := opts.CommandGrant.BindExecution(provider, opts.Authorizer, descriptor); err != nil {
				return nil, err
			}
		}
	}
	if needsWrite && opts.SoftDelete {
		if depcheck.IsNil(opts.Restore) || depcheck.IsNil(opts.Deleted) || depcheck.IsNil(opts.Purge) {
			return nil, errors.NewCode(errors.InvalidInput, "soft-delete application requires restore, deleted-query and purge ports")
		}
	}
	if !depcheck.IsNil(opts.Audit) {
		if err := validateAuditDataSource(opts.Audit, opts.Repo, opts.Read); err != nil {
			return nil, err
		}
	}
	vocabulary := opts.Authorizer.Vocabulary().Copy()
	if err := ValidateVocabulary(vocabulary, metadata, opts.Read); err != nil {
		return nil, err
	}
	effective := opts.Read.ExecutionDescriptor()
	for _, action := range descriptor.Actions {
		if action.Kind != auth.KindTransfer {
			continue
		}
		var zero T
		if _, ok := any(zero).(domain.ISettableVersion); !ok {
			return nil, errors.NewCode(errors.InvalidInput, "transfer action requires entity version support")
		}
		if opts.Hooks.For(action.Name).Before != nil {
			return nil, errors.NewCode(errors.InvalidInput, "transfer supports After and PostCommit hooks; repository owns candidate construction")
		}
		if _, ok := opts.Repo.(domcrud.ITransferRepository[T, ID]); !ok {
			return nil, errors.NewCode(errors.InvalidInput, "transfer action requires repository transfer capability")
		}
		if action.ReceiveAction == "" {
			if len(action.Dimensions) != 0 {
				return nil, errors.NewCode(errors.InvalidInput, "receive action must not declare transfer dimensions")
			}
			continue
		}
		receive, exists := actions[action.ReceiveAction]
		if !exists || receive.Kind != auth.KindTransfer || receive.ReceiveAction != "" || receive.Name == action.Name || len(action.Dimensions) == 0 {
			return nil, errors.NewCode(errors.InvalidInput, "transfer action must declare a distinct receive action and dimensions")
		}
		seen := map[meta.Dimension]bool{}
		for _, dimension := range action.Dimensions {
			if seen[dimension] || !effective.CanTransfer(dimension) {
				return nil, errors.NewCode(errors.InvalidInput, "transfer action has an invalid dimension capability")
			}
			seen[dimension] = true
		}
	}
	return &Core[T, ID]{
		commandGrant: opts.CommandGrant,
		resource:     descriptor.Resource,
		authorizer:   opts.Authorizer,
		vocabulary:   vocabulary,
		descriptor:   descriptor,
		actions:      actions,
		hooks:        freezeHooks(opts.Hooks),
		validator:    opts.Validator,
		limits:       opts.Limits.Normalize(),
		projector:    opts.Projector,
		metadata:     metadata,
		execution:    effective,
		repo:         opts.Repo,
		read:         opts.Read,
		tx:           opts.Tx,
		audit:        opts.Audit,
		softDelete:   opts.SoftDelete,
		restore:      opts.Restore,
		deleted:      opts.Deleted,
		purge:        opts.Purge,
	}, nil
}

// Descriptor 返回构造期冻结的资源动作清单。
func (c *Core[T, ID]) Descriptor() auth.Descriptor {
	out := c.descriptor
	out.Actions = append([]auth.Action(nil), c.descriptor.Actions...)
	for i := range out.Actions {
		out.Actions[i].Dimensions = append([]meta.Dimension(nil), out.Actions[i].Dimensions...)
	}
	return out
}

// MaxPageSize 返回单页上限。
func (c *Core[T, ID]) MaxPageSize() int { return c.limits.MaxPageSize }

// MaxBulkSize 返回批量上限。
func (c *Core[T, ID]) MaxBulkSize() int { return c.limits.MaxBulkSize }

// UsesAllowAuthorizer 报告当前应用是否使用一律放行的授权器（D17 启动清单）。
func (c *Core[T, ID]) UsesAllowAuthorizer() bool { return auth.IsAllow(c.authorizer) }

// AuthorizationPlan 返回执行入口的声明计划。
func (c *Core[T, ID]) AuthorizationPlan() (auth.Plan, bool) { return auth.PlanOf(c.authorizer) }

// AuthorizationBinding 记录已在构造期验证的资源与条件执行契约。
func (c *Core[T, ID]) AuthorizationBinding(action, source string) (auth.EntryBinding, error) {
	return auth.BindExecution(c.authorizer, c.descriptor, auth.ResourceAccessData, source, "Application -> Repository(spec, assignments, version)", action)
}

// FastDeny 执行声明计划提供的安全提前拒绝。
func (c *Core[T, ID]) FastDeny(ctx context.Context, action string) error {
	return auth.CheckFastDeny(ctx, c.authorizer, action)
}

// BuildDescriptor 构造并校验资源动作清单；defaults 与 custom 合并后按名称去重。
func BuildDescriptor(resource string, defaults, custom []auth.Action) (auth.Descriptor, map[string]auth.Action, error) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return auth.Descriptor{}, nil, errors.NewCode(errors.InvalidInput, "resource cannot be empty")
	}
	descriptor := auth.Descriptor{Resource: resource}
	actions := make(map[string]auth.Action, len(defaults)+len(custom))
	add := func(action auth.Action, requireKind bool) error {
		action.Dimensions = append([]meta.Dimension(nil), action.Dimensions...)
		action.Name = strings.TrimSpace(action.Name)
		if action.Name == "" {
			return errors.NewCode(errors.InvalidInput, "action name cannot be empty")
		}
		if _, exists := actions[action.Name]; exists {
			return errors.NewCode(errors.InvalidInput, "duplicate action").WithContext("action", action.Name)
		}
		if requireKind && !validKind(action.Kind) {
			return errors.NewCode(errors.InvalidInput, "action kind is invalid").
				WithContext("action", action.Name).
				WithContext("kind", string(action.Kind))
		}
		actions[action.Name] = action
		descriptor.Actions = append(descriptor.Actions, action)
		return nil
	}
	for _, action := range defaults {
		if err := add(action, true); err != nil {
			return auth.Descriptor{}, nil, err
		}
	}
	for _, action := range custom {
		if err := add(action, true); err != nil {
			return auth.Descriptor{}, nil, err
		}
	}
	if err := descriptor.Validate(); err != nil {
		return auth.Descriptor{}, nil, err
	}
	return descriptor, actions, nil
}

func validKind(kind auth.Kind) bool {
	switch kind {
	case auth.KindRead, auth.KindList, auth.KindCreate, auth.KindUpdate, auth.KindDelete, auth.KindTransfer:
		return true
	default:
		return false
	}
}

func freezeHooks[T any](hooks Hooks[T]) Hooks[T] {
	if hooks == nil {
		return nil
	}
	out := make(Hooks[T], len(hooks))
	for action, stage := range hooks {
		out[action] = stage
	}
	return out
}

// validateAuditDataSource 要求审计存储与业务仓储声明同一份数据源（D7、D17）。
func validateAuditDataSource[T domain.IEntity[ID], ID comparable](
	audit app.IAuditRepository,
	repo domcrud.IRepository[T, ID],
	read ReadRepository[T, ID],
) error {
	auditProvider := audit
	if depcheck.IsNil(auditProvider) || strings.TrimSpace(auditProvider.DataSourceID()) == "" {
		return errors.NewCode(errors.InvalidInput, "audit store must declare its data source")
	}
	var business any = repo
	if depcheck.IsNil(repo) {
		business = read
	}
	provider, ok := business.(domcrud.IDataSourceProvider)
	if !ok || depcheck.IsNil(provider) || strings.TrimSpace(provider.DataSourceID()) == "" {
		return errors.NewCode(errors.InvalidInput, "repository must declare its data source")
	}
	if provider.DataSourceID() != auditProvider.DataSourceID() {
		return errors.NewCode(errors.InvalidInput, "audit store and repository must share the same data source").
			WithContext("audit_data_source", auditProvider.DataSourceID()).
			WithContext("repository_data_source", provider.DataSourceID())
	}
	return nil
}

// ValidateVocabulary 校验授权词汇只引用实体已知字段与仓储已注册的命名条件。
func ValidateVocabulary(vocabulary auth.Vocabulary, metadata *meta.Metadata, provider app.IExecutionDescriptorProvider) error {
	if depcheck.IsNil(provider) {
		return errors.NewCode(errors.InvalidInput, "repository must expose its effective execution contract")
	}
	d := provider.ExecutionDescriptor()
	if d.Metadata() == nil || d.Metadata().Type() != metadata.Type() {
		return errors.NewCode(errors.InvalidInput, "execution contract entity type does not match application")
	}
	return d.ValidateVocabulary(vocabulary)
}

// ValidateIDType 校验实体 ID 为 string 或整数类型（含别名）。
func ValidateIDType[ID comparable]() error {
	typ := reflect.TypeOf((*ID)(nil)).Elem()
	switch typ.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	default:
		return errors.NewCode(errors.InvalidInput, "entity ID type must be string or integer").
			WithContext("id_type", typ.String())
	}
}

// FormatID 返回 ID 的规范字符串形式。
func FormatID[ID comparable](id ID) string {
	value := reflect.ValueOf(id)
	switch value.Kind() {
	case reflect.String:
		return strings.TrimSpace(value.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(value.Uint(), 10)
	default:
		return fmt.Sprint(id)
	}
}

func (c *Core[T, ID]) canonicalIDs(ids []ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, FormatID(id))
	}
	return out
}

func (c *Core[T, ID]) authorize(ctx context.Context, action string, ids []ID) (spec.Spec, auth.Decision, error) {
	if c.commandGrant != nil {
		decision, err := c.commandGrant.ExecutionDecision(ctx, c.resource, action, c.canonicalIDs(ids))
		if err != nil {
			return spec.Spec{}, auth.Decision{}, err
		}
		return decision.Spec, decision, nil
	}
	return AuthorizeAction(ctx, c.resource, c.authorizer, c.descriptor, c.actions, c.vocabulary, c.metadata, action, c.canonicalIDs(ids))
}

func (c *Core[T, ID]) authorizeKind(ctx context.Context, action string, ids []ID, kinds ...auth.Kind) (spec.Spec, auth.Decision, error) {
	if c.commandGrant != nil {
		declared, ok := c.actions[action]
		if ok {
			for _, kind := range kinds {
				if kind == declared.Kind {
					return c.authorize(ctx, action, ids)
				}
			}
		}
		return spec.Spec{}, auth.Decision{}, errors.NewCode(errors.Forbidden, "action kind is outside the command execution")
	}
	return AuthorizeKind(ctx, c.resource, c.authorizer, c.descriptor, c.actions, c.vocabulary, c.metadata, action, c.canonicalIDs(ids), kinds...)
}

// AuthorizeAction 按构造期冻结的资源、动作与词汇执行一次授权判定。
func AuthorizeAction(
	ctx context.Context,
	resource string,
	authorizer auth.IAuthorizer,
	descriptor auth.Descriptor,
	actions map[string]auth.Action,
	vocabulary auth.Vocabulary,
	metadata *meta.Metadata,
	action string,
	ids []string,
	targets ...auth.Assignment,
) (spec.Spec, auth.Decision, error) {
	declared, ok := actions[action]
	if !ok {
		return spec.Spec{}, auth.Decision{}, errors.NewCode(errors.Forbidden, "action is not declared").
			WithContext("resource", resource).
			WithContext("action", action)
	}
	permission, ok := descriptor.PermissionFor(declared.Name)
	if !ok {
		return spec.Spec{}, auth.Decision{}, errors.NewCode(errors.Internal, "action has no permission code").
			WithContext("action", action)
	}
	decision, err := authorizer.Authorize(ctx, auth.Request{
		Resource:   resource,
		Action:     action,
		Permission: permission,
		IDs:        ids,
		Targets:    append([]auth.Assignment(nil), targets...),
	})
	if err != nil {
		return spec.Spec{}, auth.Decision{}, errors.Wrap(err, errors.Code(err), "authorization failed").
			WithContext("action", action)
	}
	if !decision.Allowed {
		return spec.Spec{}, auth.Decision{}, errors.NewCode(errors.Forbidden, "access denied").
			WithContext("action", action).
			WithContext("reason", decision.Reason)
	}
	if err := decision.ValidateAgainst(vocabulary, metadata); err != nil {
		return spec.Spec{}, auth.Decision{}, err
	}
	if err := decision.Spec.Err(); err != nil {
		return spec.Spec{}, auth.Decision{}, errors.Wrap(err, errors.Internal, "authorizer returned an invalid spec")
	}
	return decision.Spec, decision, nil
}

// AuthorizeKind 校验动作 Kind 后执行授权判定。
func AuthorizeKind(
	ctx context.Context,
	resource string,
	authorizer auth.IAuthorizer,
	descriptor auth.Descriptor,
	actions map[string]auth.Action,
	vocabulary auth.Vocabulary,
	metadata *meta.Metadata,
	action string,
	ids []string,
	kinds ...auth.Kind,
) (spec.Spec, auth.Decision, error) {
	declared, ok := actions[action]
	if !ok {
		return spec.Spec{}, auth.Decision{}, errors.NewCode(errors.Forbidden, "action is not declared").
			WithContext("resource", resource).
			WithContext("action", action)
	}
	matched := false
	for _, kind := range kinds {
		if declared.Kind == kind {
			matched = true
			break
		}
	}
	if !matched {
		return spec.Spec{}, auth.Decision{}, errors.NewCode(errors.Internal, "action kind does not match the entry point").
			WithContext("action", action).
			WithContext("kind", string(declared.Kind))
	}
	return AuthorizeAction(ctx, resource, authorizer, descriptor, actions, vocabulary, metadata, action, ids)
}

func (c *Core[T, ID]) applyAssignments(entity T, assignments []auth.Assignment) error {
	return app.ApplyAssignments(entity, assignments, c.execution)
}

// ValidateAssignments 校验实体已满足服务端权威赋值，不修改实体。
//
// 事件溯源不能回写聚合，命令必须自行产出赋值结果；这里只做一致性核对，
// 不一致按 Forbidden 拒绝（D3、D9）。
func ValidateAssignments(entity any, assignments []auth.Assignment, metadata *meta.Metadata) error {
	for _, assignment := range assignments {
		if err := assignment.Validate(); err != nil {
			return err
		}
		field, ok := metadata.DimensionField(assignment.Dimension)
		if !ok {
			return errors.NewCode(errors.Internal, "entity does not expose the assigned role").
				WithContext("role", string(assignment.Dimension))
		}
		converted, err := field.Convert(assignment.Value)
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "assignment value conversion failed").
				WithContext("role", string(assignment.Dimension))
		}
		current, ok := field.Value(entity)
		if !ok {
			return errors.NewCode(errors.Internal, "assignment target field cannot be read").
				WithContext("role", string(assignment.Dimension))
		}
		for current.Kind() == reflect.Interface && !current.IsNil() {
			current = current.Elem()
		}
		if !reflect.DeepEqual(current.Interface(), converted.Interface()) {
			return errors.NewCode(errors.Forbidden, "assignment does not match the command result").
				WithContext("role", string(assignment.Dimension))
		}
	}
	return nil
}

func (c *Core[T, ID]) validate(entity T) error {
	if validatable, ok := any(entity).(domain.IValidatable); ok {
		if err := validatable.Validate(); err != nil {
			return err
		}
	}
	if c.validator != nil {
		return c.validator.Validate(entity)
	}
	return nil
}

func (c *Core[T, ID]) normalizePage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = c.limits.MaxPageSize
	}
	if limit > c.limits.MaxPageSize {
		limit = c.limits.MaxPageSize
	}
	return offset, limit
}

func (c *Core[T, ID]) invokeBefore(ctx context.Context, stage Stage[T], entity T) error {
	if stage.Before == nil {
		return nil
	}
	if _, err := meta.Clone(entity); err != nil {
		return err
	}
	return stage.Before(ctx, entity)
}

func (c *Core[T, ID]) invokeAfter(ctx context.Context, stage Stage[T], entity T) error {
	if stage.After == nil {
		return nil
	}
	view, err := meta.Clone(entity)
	if err != nil {
		return err
	}
	return stage.After(ctx, view)
}

// schedulePostCommit 在事务提交后执行钩子；txCtx 只用于登记回调，
// 回调本身使用命令入口的 ctx，避免看到已结束的事务值。
func (c *Core[T, ID]) schedulePostCommit(txCtx, runCtx context.Context, stage Stage[T], entity T) error {
	if stage.PostCommit == nil {
		return nil
	}
	view, err := meta.Clone(entity)
	if err != nil {
		return err
	}
	return contextx.AppendAfterCommit(txCtx, func(context.Context) error {
		return stage.PostCommit(runCtx, view)
	})
}

func (c *Core[T, ID]) scheduleBulkPostCommit(txCtx context.Context, callbacks []func() error) error {
	if len(callbacks) == 0 {
		return nil
	}
	return contextx.AppendAfterCommit(txCtx, func(context.Context) error {
		var errs []error
		for _, callback := range callbacks {
			if err := callback(); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) == 0 {
			return nil
		}
		return joinErrors(errs)
	})
}

func marshalJSON(value any, message string) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, message)
	}
	return data, nil
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinErrors(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	msgs := make([]string, 0, len(errs))
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}
	return errors.NewCode(errors.Internal, strings.Join(msgs, "; "))
}

func nowUTC() time.Time { return time.Now() }
