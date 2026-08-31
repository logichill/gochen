package contextx

import (
	"context"
	"fmt"
	"time"

	"gochen/gen/uuid"
)

// Background 返回一个“最小可追踪”的兜底 context。
func Background() context.Context {
	base := context.Background()
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
	return generateTraceID(uuid.NewV7, time.Now)
}

func generateTraceID(nextID func() (string, error), now func() time.Time) string {
	id, err := nextID()
	if err != nil || id == "" {
		return fmt.Sprintf("trc-%d", now().UnixNano())
	}
	return "trc-" + id
}
