package contextx

import (
	stdctx "context"
	"strings"
)

// IMetadata 表示可读写的元数据抽象（用于跨进程传播）。
type IMetadata interface {
	Get(key string) (string, bool)
	Set(key, value string)
}

// MapMetadata 定义 MapMetadata。
type MapMetadata map[string]string

// Get 返回 key 对应的值；不存在时 ok 为 false。
func (m MapMetadata) Get(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

// Set 设置 key 的值。
func (m MapMetadata) Set(key, value string) { m[key] = value }

var _ IMetadata = MapMetadata(nil)

// InjectTenantID 将当前 context 中的 tenant_id 注入到 metadata（若 metadata 未设置该字段）。
func InjectTenantID(ctx stdctx.Context, metadata IMetadata) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	if v, ok := metadata.Get(MetadataTenantKey); ok && v != "" {
		return nil
	}
	if tenantID := TenantID(ctx); tenantID != "" {
		metadata.Set(MetadataTenantKey, tenantID)
	}
	return nil
}

// InjectTraceID 将当前 context 中的 trace_id 注入到 metadata（若 metadata 未设置该字段）。
func InjectTraceID(ctx stdctx.Context, metadata IMetadata) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	if v, ok := metadata.Get(MetadataTraceKey); ok && v != "" {
		return nil
	}
	if traceID := TraceID(ctx); traceID != "" {
		metadata.Set(MetadataTraceKey, traceID)
	}
	return nil
}

// InjectRequestID 将当前 context 中的 request_id 注入到 metadata（若 metadata 未设置该字段）。
func InjectRequestID(ctx stdctx.Context, metadata IMetadata) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	if v, ok := metadata.Get(MetadataRequestIDKey); ok && strings.TrimSpace(v) != "" {
		return nil
	}
	if requestID := RequestID(ctx); requestID != "" {
		metadata.Set(MetadataRequestIDKey, requestID)
	}
	return nil
}

// InjectOperator 将当前 context 中的 operator 注入到 metadata（若 metadata 未设置该字段）。
func InjectOperator(ctx stdctx.Context, metadata IMetadata) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	if v, ok := metadata.Get(MetadataOperatorKey); ok && strings.TrimSpace(v) != "" {
		return nil
	}
	if op := Operator(ctx); op != "" {
		metadata.Set(MetadataOperatorKey, op)
	}
	return nil
}

// InjectAll 将当前 context 中的 tenant/trace/request/operator 注入到 metadata（缺失时补齐）。
func InjectAll(ctx stdctx.Context, metadata IMetadata) error {
	if err := InjectTenantID(ctx, metadata); err != nil {
		return err
	}
	if err := InjectTraceID(ctx, metadata); err != nil {
		return err
	}
	if err := InjectRequestID(ctx, metadata); err != nil {
		return err
	}
	return InjectOperator(ctx, metadata)
}

// DeriveFromMetadata 从 metadata 补齐 ctx 中的 tenant/trace/request/operator（仅当 ctx 缺失时）。
func DeriveFromMetadata(ctx stdctx.Context, metadata IMetadata) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		return ctx, nil
	}
	if TenantID(ctx) == "" {
		if v, ok := metadata.Get(MetadataTenantKey); ok && strings.TrimSpace(v) != "" {
			ctx, err = WithTenantID(ctx, v)
			if err != nil {
				return nil, err
			}
		}
	}
	if TraceID(ctx) == "" {
		if v, ok := metadata.Get(MetadataTraceKey); ok && strings.TrimSpace(v) != "" {
			ctx, err = WithTraceID(ctx, v)
			if err != nil {
				return nil, err
			}
		}
	}
	if RequestID(ctx) == "" {
		if v, ok := metadata.Get(MetadataRequestIDKey); ok && strings.TrimSpace(v) != "" {
			ctx, err = WithRequestID(ctx, v)
			if err != nil {
				return nil, err
			}
		}
	}
	if Operator(ctx) == "" {
		if v, ok := metadata.Get(MetadataOperatorKey); ok && strings.TrimSpace(v) != "" {
			ctx, err = WithOperator(ctx, v)
			if err != nil {
				return nil, err
			}
		}
	}
	return ctx, nil
}

// EnsureTraceID 确保 ctx 与 metadata 都具备 trace_id（缺失时使用 fallback 兜底）。
func EnsureTraceID(ctx stdctx.Context, metadata IMetadata, fallback string) (stdctx.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		if TraceID(ctx) == "" && strings.TrimSpace(fallback) != "" {
			return WithTraceID(ctx, fallback)
		}
		return ctx, nil
	}

	if traceID := TraceID(ctx); traceID != "" {
		if v, ok := metadata.Get(MetadataTraceKey); !ok || strings.TrimSpace(v) == "" || strings.TrimSpace(v) != traceID {
			metadata.Set(MetadataTraceKey, traceID)
		}
		return ctx, nil
	}

	if v, ok := metadata.Get(MetadataTraceKey); ok && strings.TrimSpace(v) != "" {
		return WithTraceID(ctx, v)
	}

	fb := strings.TrimSpace(fallback)
	if fb != "" {
		metadata.Set(MetadataTraceKey, fb)
		return WithTraceID(ctx, fb)
	}
	return ctx, nil
}
