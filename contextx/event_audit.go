package contextx

import (
	"context"
	"strings"

	"gochen/contextx/propagation"
)

// 事件审计元数据键名。
const (
	// MetadataInitiatorKey 定义系统代执行的发起人字段键名。
	MetadataInitiatorKey = "initiator"
	// MetadataActionKey 定义审计动作字段键名。
	MetadataActionKey = "action"
	// MetadataReasonKey 定义审计原因字段键名。
	MetadataReasonKey = "reason"
	// MetadataCorrelationIDKey 定义审计关联 ID 字段键名。
	MetadataCorrelationIDKey = "correlation_id"
)

// EventAudit 描述写入事件元数据的审计信息。
//
// 只承载标识与动作，不承载业务数据；空字段不注入。
type EventAudit struct {
	Operator      string
	Initiator     string
	Action        string
	Reason        string
	CorrelationID string
}

type eventAuditKey struct{}

// WithEventAudit 将审计信息写入 context。
func WithEventAudit(ctx context.Context, audit EventAudit) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, eventAuditKey{}, audit)
}

// EventAuditFromContext 读取 context 中的审计信息。
func EventAuditFromContext(ctx context.Context) (EventAudit, bool) {
	if ctx == nil {
		return EventAudit{}, false
	}
	audit, ok := ctx.Value(eventAuditKey{}).(EventAudit)
	return audit, ok
}

// InjectEventAudit 将 context 中的审计字段注入 metadata。
//
// 只有写入 EventAudit 的上下文才会注入，直接操作仓储的调用保持原行为。
// operator 以 EventAudit 优先，缺失时回退 ctx 中的 operator；
// correlation_id 缺失时回退 request_id、trace_id。空值不覆盖。
func InjectEventAudit(ctx context.Context, metadata propagation.ICarrier) error {
	if _, err := Ensure(ctx); err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	audit, ok := EventAuditFromContext(ctx)
	if !ok {
		return nil
	}
	operator := strings.TrimSpace(audit.Operator)
	if operator == "" {
		operator = Operator(ctx)
	}
	correlationID := strings.TrimSpace(audit.CorrelationID)
	if correlationID == "" {
		correlationID = RequestID(ctx)
	}
	if correlationID == "" {
		correlationID = TraceID(ctx)
	}
	values := []struct {
		key   string
		value string
	}{
		{MetadataOperatorKey, operator},
		{MetadataInitiatorKey, strings.TrimSpace(audit.Initiator)},
		{MetadataActionKey, strings.TrimSpace(audit.Action)},
		{MetadataReasonKey, strings.TrimSpace(audit.Reason)},
		{MetadataCorrelationIDKey, correlationID},
	}
	for _, item := range values {
		if item.value == "" {
			continue
		}
		if v, ok := metadata.Get(item.key); !ok || strings.TrimSpace(v) != item.value {
			metadata.Set(item.key, item.value)
		}
	}
	return nil
}
