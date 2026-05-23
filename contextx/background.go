package contextx

import (
	stdctx "context"
	"fmt"
	"time"

	"gochen/ident/snowflake"
)

// Background 返回一个“最小可追踪”的兜底 context。
func Background() stdctx.Context {
	base := stdctx.Background()
	if TraceID(base) != "" {
		return base
	}

	ctx, err := WithTraceID(base, GenerateTraceID())
	if err != nil || ctx == nil {
		return base
	}
	return ctx
}

// GenerateTraceID 生成新的 trace_id。
func GenerateTraceID() string {
	id, err := snowflake.NextID()
	if err != nil || id == 0 {
		id = time.Now().UnixNano()
	}
	return fmt.Sprintf("trc-%d", id)
}
