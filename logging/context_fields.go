package logging

import (
	"context"

	"gochen/contextx/fields"
)

// ContextFields 从 ctx 中提取标准化的链路字段（若存在）。
//
// 说明：
// - 用于日志输出的统一维度：tenant_id/trace_id/operator；
// - 仅在值非空时返回对应字段。
func ContextFields(ctx context.Context) []Field {
	if ctx == nil {
		return nil
	}
	var out []Field
	if v := fields.TenantID(ctx); v != "" {
		out = append(out, String(fields.MetadataTenantKey, v))
	}
	if v := fields.TraceID(ctx); v != "" {
		out = append(out, String(fields.MetadataTraceKey, v))
	}
	if v := fields.Operator(ctx); v != "" {
		out = append(out, String(fields.MetadataOperatorKey, v))
	}
	return out
}

// mergeContextFields 合并上下文字段集合。
func mergeContextFields(ctx context.Context, fields []Field) []Field {
	ctxFields := ContextFields(ctx)
	if len(ctxFields) == 0 {
		return fields
	}
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		seen[f.Key] = struct{}{}
	}
	for _, f := range ctxFields {
		if _, ok := seen[f.Key]; ok {
			continue
		}
		fields = append(fields, f)
	}
	return fields
}
