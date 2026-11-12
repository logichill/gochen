// Package audit 提供独立于资源读取和删除的审计历史用例。
package audit

import (
	"context"
	"gochen/app"
	"gochen/app/internal/execution"
	records "gochen/audit"
	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// ActionRead 是审计历史读取动作。
const ActionRead = "audit_read"

// IAuditReader 是独立的审计读取能力。
type IAuditReader[ID comparable] interface {
	AuditTrail(context.Context, ID, int, int) ([]records.AuditRecord, error)
}

// Options 配置审计读取授权和分页上限。
type Options struct {
	Resource   string
	Authorizer auth.IAuthorizer
	Permission string
	Limits     app.Limits
}

// Reader 只读取历史快照，不读取当前资源行。
type Reader[ID comparable] struct {
	store      records.IAuditQueryStore
	authorizer auth.IAuthorizer
	descriptor auth.Descriptor
	actions    map[string]auth.Action
	metadata   *meta.Metadata
	vocabulary auth.Vocabulary
	limits     app.Limits
}

// Describe 返回审计读取的独立动作描述。
func Describe(options Options) (auth.Descriptor, error) {
	d, _, err := execution.BuildDescriptor(options.Resource, []auth.Action{{Name: ActionRead, Kind: auth.KindRead, Permission: options.Permission}}, nil)
	return d, err
}

// NewReader 绑定审计查询和资源元数据，验证历史维度的授权词汇。
func NewReader[ID comparable](store records.IAuditQueryStore, provider app.IExecutionDescriptorProvider, options Options) (*Reader[ID], error) {
	if depcheck.IsNil(store) || depcheck.IsNil(provider) || depcheck.IsNil(options.Authorizer) {
		return nil, errors.NewCode(errors.InvalidInput, "audit reader requires store, execution descriptor and authorizer")
	}
	if err := execution.ValidateIDType[ID](); err != nil {
		return nil, err
	}
	descriptor, actions, err := execution.BuildDescriptor(options.Resource, []auth.Action{{Name: ActionRead, Kind: auth.KindRead, Permission: options.Permission}}, nil)
	if err != nil {
		return nil, err
	}
	if err := auth.ValidatePlanBinding(options.Authorizer, descriptor, auth.ResourceAccessData); err != nil {
		return nil, err
	}
	metadata := provider.ExecutionDescriptor().Metadata()
	vocabulary := options.Authorizer.Vocabulary().Copy()
	if metadata == nil {
		return nil, errors.NewCode(errors.InvalidInput, "audit reader requires entity metadata")
	}
	if err := execution.ValidateVocabulary(vocabulary, metadata, provider); err != nil {
		return nil, err
	}
	return &Reader[ID]{store: store, authorizer: options.Authorizer, descriptor: descriptor, actions: actions, metadata: metadata, vocabulary: vocabulary, limits: options.Limits.Normalize()}, nil
}

// Descriptor 返回独立动作清单。
func (r *Reader[ID]) Descriptor() auth.Descriptor { return r.descriptor.Copy() }

// MaxPageSize 返回分页上限。
func (r *Reader[ID]) MaxPageSize() int { return r.limits.MaxPageSize }

// UsesAllowAuthorizer 表明是否使用一律放行授权。
func (r *Reader[ID]) UsesAllowAuthorizer() bool { return auth.IsAllow(r.authorizer) }

// AuthorizationPlan 返回声明授权计划。
func (r *Reader[ID]) AuthorizationPlan() (auth.Plan, bool) { return auth.PlanOf(r.authorizer) }

// AuthorizationBinding 返回经过验证的授权绑定。
func (r *Reader[ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	return auth.BindExecution(r.authorizer, r.descriptor, auth.ResourceAccessData, "app/audit.Reader", "AuditStore(snapshot spec)", action)
}

// FastDeny 执行安全的提前拒绝。
func (r *Reader[ID]) FastDeny(ctx context.Context, action string) error {
	return auth.CheckFastDeny(ctx, r.authorizer, action)
}

// AuditTrail 按历史维度查询；实体永久删除后仍可使用。
func (r *Reader[ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]records.AuditRecord, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, _, err := execution.AuthorizeKind(ctx, r.descriptor.Resource, r.authorizer, r.descriptor, r.actions, r.vocabulary, r.metadata, ActionRead, []string{execution.FormatID(id)}, auth.KindRead)
	if err != nil {
		return nil, err
	}
	if err := execution.ValidateAuditSpec(s, r.metadata); err != nil {
		return nil, err
	}
	s = spec.And(s, spec.Eq(meta.ID(), execution.FormatID(id)))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > r.limits.MaxPageSize {
		limit = r.limits.MaxPageSize
	}
	return r.store.ListAuditRecords(records.WithAuditResourceKind(ctx, r.descriptor.Resource), execution.FormatID(id), offset, limit, s)
}
