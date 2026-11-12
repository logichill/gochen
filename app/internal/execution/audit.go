package execution

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"gochen/app/operation"
	domaudited "gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// ExecOption 配置一次命令执行的审计元数据。
type ExecOption func(*execOptions)

type execOptions struct {
	reason string
}

// WithReason 设置本次命令的审计原因。
func WithReason(reason string) ExecOption {
	return func(opts *execOptions) {
		if opts == nil {
			return
		}
		opts.reason = strings.TrimSpace(reason)
	}
}

// Actor 是一次命令的审计执行者信息。
type Actor struct {
	Operator      string
	Initiator     string
	Reason        string
	CorrelationID string
}

// ResolveActor 从 ctx 与 ExecOption 解析审计执行者；系统身份代为执行时
// Operator 为 system，Initiator 记录发起人（D11、D13）。
//
// requireOperator 为 true 时缺少操作人返回 InvalidInput；事件溯源把审计
// 信息写进事件元数据，允许旧上下文没有操作人。
func ResolveActor(ctx context.Context, opts []ExecOption, requireOperator bool) (Actor, error) {
	var cfg execOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	resolved := Actor{Reason: cfg.reason}
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		if principal.System {
			resolved.Operator = SystemOperator
			if info, ok := auth.SystemInfoFromContext(ctx); ok {
				resolved.Initiator = strings.TrimSpace(info.Initiator)
				if resolved.Reason == "" {
					resolved.Reason = strings.TrimSpace(info.Reason)
				}
			}
			if resolved.Initiator == "" {
				resolved.Initiator = strings.TrimSpace(principal.SubjectID)
			}
		} else if subject := strings.TrimSpace(principal.SubjectID); subject != "" {
			resolved.Operator = subject
		}
	}
	if resolved.Operator == "" {
		resolved.Operator = strings.TrimSpace(contextx.Operator(ctx))
	}
	if resolved.Operator == "" && requireOperator {
		return Actor{}, errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	resolved.CorrelationID = contextx.RequestID(ctx)
	if resolved.CorrelationID == "" {
		resolved.CorrelationID = contextx.TraceID(ctx)
	}
	return resolved, nil
}

// auditActor 始终保留可用的执行者归因；只有写审计记录时才强制要求操作人。
func (c *Core[T, ID]) auditActor(ctx context.Context, opts []ExecOption) (Actor, error) {
	return ResolveActor(ctx, opts, !depcheck.IsNil(c.audit))
}

func (c *Core[T, ID]) project(entity T) (any, error) {
	if c.projector == nil {
		return entity, nil
	}
	view, err := meta.Clone(entity)
	if err != nil {
		return nil, err
	}
	return c.projector(view)
}

// snapshot 在指定时刻生成审计快照；调用方必须在实体被修改前立即调用。
func (c *Core[T, ID]) snapshot(entity T) (json.RawMessage, error) {
	target, err := c.project(entity)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "audit projection failed")
	}
	return marshalJSON(target, "marshal audit snapshot")
}

func (c *Core[T, ID]) record(ctx context.Context, act Actor, action string, entity T, changes json.RawMessage) (domaudited.AuditRecord, error) {
	dimensions, err := domaudited.CaptureDimensions(c.metadata, entity)
	if err != nil {
		return domaudited.AuditRecord{}, err
	}
	return domaudited.AuditRecord{
		ResourceKind:  c.resource,
		EntityID:      FormatID(entity.GetID()),
		Action:        action,
		Operator:      act.Operator,
		Initiator:     act.Initiator,
		Reason:        act.Reason,
		CorrelationID: act.CorrelationID,
		OperationID:   operation.OperationIDFromContext(ctx),
		Dimensions:    dimensions,
		Timestamp:     nowUTC(),
		Changes:       changes,
	}, nil
}

func (c *Core[T, ID]) saveAudit(ctx context.Context, record domaudited.AuditRecord) error {
	if depcheck.IsNil(c.audit) {
		return nil
	}
	_, err := c.audit.SaveAuditRecord(ctx, record)
	return err
}

func (c *Core[T, ID]) saveAudits(ctx context.Context, records []domaudited.AuditRecord) error {
	if depcheck.IsNil(c.audit) || len(records) == 0 {
		return nil
	}
	if bulk, ok := any(c.audit).(domaudited.IBulkAuditStore); ok && !depcheck.IsNil(bulk) {
		return bulk.SaveAuditRecords(ctx, records)
	}
	for i := range records {
		if _, err := c.audit.SaveAuditRecord(ctx, records[i]); err != nil {
			return err
		}
	}
	return nil
}

// diffSnapshots 计算两份序列化快照之间的业务字段差异。
func (c *Core[T, ID]) diffSnapshots(before, after json.RawMessage) (json.RawMessage, error) {
	beforeMap, err := unmarshalSnapshot(before)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "decode audit diff before snapshot")
	}
	afterMap, err := unmarshalSnapshot(after)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "decode audit diff after snapshot")
	}

	ignored := c.ignoredAuditFields()
	keySet := make(map[string]struct{}, len(beforeMap)+len(afterMap))
	for key := range beforeMap {
		keySet[key] = struct{}{}
	}
	for key := range afterMap {
		keySet[key] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var diffs []domaudited.FieldChange
	for _, key := range keys {
		if _, skip := ignored[normalizeFieldName(key)]; skip {
			continue
		}
		oldVal, oldExists := beforeMap[key]
		newVal, newExists := afterMap[key]
		switch {
		case !oldExists && !newExists:
			continue
		case !oldExists:
			if newVal == nil {
				continue
			}
			diffs = append(diffs, domaudited.FieldChange{Field: key, Old: nil, New: newVal})
		case !newExists:
			if oldVal == nil {
				continue
			}
			diffs = append(diffs, domaudited.FieldChange{Field: key, Old: oldVal, New: nil})
		case !reflect.DeepEqual(oldVal, newVal):
			diffs = append(diffs, domaudited.FieldChange{Field: key, Old: oldVal, New: newVal})
		}
	}
	if len(diffs) == 0 {
		return nil, nil
	}
	return marshalJSON(diffs, "marshal audit diff")
}

// ignoredAuditFields 返回应由框架托管、不进入业务审计差异的字段名（归一键）。
func (c *Core[T, ID]) ignoredAuditFields() map[string]struct{} {
	fields := c.metadata.Fields()
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !field.Identity && !field.Managed {
			continue
		}
		out[normalizeFieldName(field.Name)] = struct{}{}
	}
	return out
}

func normalizeFieldName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", ""))
}

func unmarshalSnapshot(data json.RawMessage) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// ValidateAuditSpec 校验审计规约引用实体标识或维度字段。
func ValidateAuditSpec(s spec.Spec, metadata *meta.Metadata) error {
	if err := s.Err(); err != nil {
		return errors.Wrap(err, errors.Internal, "authorizer returned an invalid audit spec")
	}
	if s.HasNamed() {
		return errors.NewCode(errors.Internal, "audit spec must not contain named conditions")
	}
	for _, ref := range s.Refs() {
		field, err := metadata.Resolve(ref)
		if err != nil {
			return err
		}
		if !field.Identity && field.Dimension == "" {
			return errors.NewCode(errors.Internal, "audit spec references a disallowed field").
				WithContext("reference", ref.String())
		}
	}
	return nil
}
