package contextx

import (
	"context"
	"strings"

	"gochen/contextx/propagation"
)

// InjectTenantID 将当前 context 中的 tenant_id 注入到 metadata（ctx 有值时以 ctx 为准）。
func InjectTenantID(ctx context.Context, metadata propagation.ICarrier) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	tenantID := TenantID(ctx)
	if tenantID != "" {
		if v, ok := metadata.Get(MetadataTenantKey); !ok || strings.TrimSpace(v) != tenantID {
			metadata.Set(MetadataTenantKey, tenantID)
		}
		return nil
	}
	return nil
}

// InjectTraceID 将当前 context 中的 trace_id 注入到 metadata（若 metadata 未设置该字段）。
func InjectTraceID(ctx context.Context, metadata propagation.ICarrier) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	if v, ok := metadata.Get(MetadataTraceKey); ok && strings.TrimSpace(v) != "" {
		return nil
	}
	if traceID := TraceID(ctx); traceID != "" {
		metadata.Set(MetadataTraceKey, traceID)
	}
	return nil
}

// InjectRequestID 将当前 context 中的 request_id 注入到 metadata（若 metadata 未设置该字段）。
func InjectRequestID(ctx context.Context, metadata propagation.ICarrier) error {
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

// InjectOperator 将当前 context 中的 operator 注入到 metadata（ctx 有值时以 ctx 为准）。
func InjectOperator(ctx context.Context, metadata propagation.ICarrier) error {
	_, err := Ensure(ctx)
	if err != nil {
		return err
	}
	if metadata == nil {
		return nil
	}
	op := Operator(ctx)
	if op != "" {
		if v, ok := metadata.Get(MetadataOperatorKey); !ok || strings.TrimSpace(v) != op {
			metadata.Set(MetadataOperatorKey, op)
		}
		return nil
	}
	return nil
}

// InjectAll 将当前 context 中的 tenant/trace/request/operator 注入到 metadata。
//
// tenant/operator 在 ctx 有值时以 ctx 为准，会覆盖缺失或不一致的 metadata；
// trace/request 仅在 metadata 缺失时补齐。
func InjectAll(ctx context.Context, metadata propagation.ICarrier) error {
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
func DeriveFromMetadata(ctx context.Context, metadata propagation.ICarrier) (context.Context, error) {
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
func EnsureTraceID(ctx context.Context, metadata propagation.ICarrier, fallback string) (context.Context, error) {
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
