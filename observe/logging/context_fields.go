package logging

import (
	"context"

	"gochen/contextx/field"
)

// ContextFields 从 ctx 中提取标准化的链路字段（若存在）。
//
// 说明：
// - 用于日志输出的统一维度：tenant_id/trace_id/request_id/operator；
// - 仅在值非空时返回对应字段。
func ContextFields(ctx context.Context) []Field {
	if ctx == nil {
		return nil
	}
	var out []Field
	if v := field.TenantID(ctx); v != "" {
		out = append(out, String(field.MetadataTenantKey, v))
	}
	if v := field.TraceID(ctx); v != "" {
		out = append(out, String(field.MetadataTraceKey, v))
	}
	if v := field.RequestID(ctx); v != "" {
		out = append(out, String(field.MetadataRequestIDKey, v))
	}
	if v := field.Operator(ctx); v != "" {
		out = append(out, String(field.MetadataOperatorKey, v))
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
