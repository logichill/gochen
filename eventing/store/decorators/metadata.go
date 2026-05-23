package decorators

import (
	"context"
	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"strings"
)

// ensureBatchTraceID 确保批量追踪ID。
func ensureBatchTraceID[ID comparable](ctx context.Context, events []eventing.IStorableEvent[ID]) (context.Context, string, error) {
	if ctx == nil {
		return nil, "", errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	traceSet := make(map[string]struct{})

	for _, evt := range events {
		if evt == nil {
			continue
		}
		md := evt.GetMetadata()
		if md == nil {
			continue
		}
		if v, ok := md.Get(contextx.MetadataTraceKey); ok {
			v = strings.TrimSpace(v)
			if v != "" {
				traceSet[v] = struct{}{}
			}
		}
	}

	ctxTrace := strings.TrimSpace(contextx.TraceID(ctx))

	traceID := ctxTrace
	if traceID == "" {
		switch len(traceSet) {
		case 0:
			traceID = ""
		case 1:
			for v := range traceSet {
				traceID = v
			}
		default:
			return nil, "", errors.NewCode(errors.InvalidInput, "inconsistent trace_id in event batch").
				WithContext("distinct_count", len(traceSet))
		}
	}

	if ctxTrace == "" && traceID != "" {
		derived, err := contextx.WithTraceID(ctx, traceID)
		if err != nil {
			return nil, "", err
		}
		ctx = derived
	}

	for _, evt := range events {
		if evt == nil {
			continue
		}
		md := evt.GetMetadata()
		if md == nil {
			continue
		}
		if traceID != "" {
			if v, ok := md.Get(contextx.MetadataTraceKey); ok && strings.TrimSpace(v) != "" && strings.TrimSpace(v) != traceID {
				return nil, "", errors.NewCode(errors.InvalidInput, "event trace_id conflicts with batch trace_id").
					WithContext("event_id", evt.GetID()).
					WithContext("trace_id", strings.TrimSpace(v)).
					WithContext("batch_trace_id", traceID)
			}
			md.Set(contextx.MetadataTraceKey, traceID)
		}
	}

	return ctx, traceID, nil
}
