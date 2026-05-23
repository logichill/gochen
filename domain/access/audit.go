package access

import "context"

// WriteAuditRecord 表达一次显式写入约束落库事件。
type WriteAuditRecord struct {
	Operation  string
	Constraint WriteConstraint
	Resources  []ResourceConstraint
}

// IWriteAuditRecorder 定义显式写入约束审计接口。
type IWriteAuditRecorder interface {
	RecordWriteAudit(ctx context.Context, record WriteAuditRecord)
}

// WriteAuditRecorderFunc 允许用函数直接实现 IWriteAuditRecorder。
type WriteAuditRecorderFunc func(ctx context.Context, record WriteAuditRecord)

// RecordWriteAudit 记录写入审计。
func (f WriteAuditRecorderFunc) RecordWriteAudit(ctx context.Context, record WriteAuditRecord) {
	f(ctx, record)
}

type writeAuditRecorderContextKey struct{}

// WithWriteAuditRecorder 将写入审计器绑定到 context。
func WithWriteAuditRecorder(ctx context.Context, recorder IWriteAuditRecorder) context.Context {
	if ctx == nil || recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, writeAuditRecorderContextKey{}, recorder)
}

// WriteAuditRecorderFromContext 从 context 中读取写入审计器。
func WriteAuditRecorderFromContext(ctx context.Context) (IWriteAuditRecorder, bool) {
	if ctx == nil {
		return nil, false
	}
	recorder, ok := ctx.Value(writeAuditRecorderContextKey{}).(IWriteAuditRecorder)
	return recorder, ok && recorder != nil
}

// RecordWriteConstraintAudit 记录一次显式写入约束落库事件；若未注入 recorder，则静默跳过。
func RecordWriteConstraintAudit(ctx context.Context, operation string, constraint WriteConstraint, resources ...ResourceConstraint) {
	recorder, ok := WriteAuditRecorderFromContext(ctx)
	if !ok {
		return
	}
	recorder.RecordWriteAudit(ctx, WriteAuditRecord{
		Operation:  stringsTrim(operation),
		Constraint: normalizeWriteConstraint(constraint),
		Resources:  normalizeResourceConstraints(resources),
	})
}

func normalizeResourceConstraints(resources []ResourceConstraint) []ResourceConstraint {
	if len(resources) == 0 {
		return nil
	}
	out := make([]ResourceConstraint, 0, len(resources))
	for _, resource := range resources {
		out = append(out, normalizeResourceConstraint(resource))
	}
	return out
}
