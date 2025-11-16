package middleware

import (
	"context"
	"time"

	"gochen/messaging"
)

// TracingKeys 用于在 Metadata 与 Context 中传播的字段名
const (
	KeyCorrelationID = "correlation_id"
	KeyCausationID   = "causation_id"
	KeyTraceID       = "trace_id"
)

// TracingMiddleware 注入并传播 correlation_id/causation_id/trace_id
//
// 规则：
// - command：若缺失 correlation_id/trace_id，则设置为消息ID；将三者放入 Context 以便后续事件沿用
// - event：若缺失，则优先从 Context 继承；仍缺失则使用消息ID兜底
type TracingMiddleware struct{}

func NewTracingMiddleware() *TracingMiddleware { return &TracingMiddleware{} }

func (m *TracingMiddleware) Name() string { return "Tracing" }

func (m *TracingMiddleware) Handle(ctx context.Context, message messaging.IMessage, next messaging.HandlerFunc) error {
	if message == nil {
		return next(ctx, message)
	}
	md := message.GetMetadata()
	msgID := message.GetID()
	msgType := message.GetType()

	// 从上下文获取已有链路信息
	ctxCorr, _ := ctx.Value(KeyCorrelationID).(string)
	ctxCaus, _ := ctx.Value(KeyCausationID).(string)
	ctxTrace, _ := ctx.Value(KeyTraceID).(string)

	switch msgType {
	case messaging.MessageTypeCommand:
		if _, ok := md[KeyCorrelationID]; !ok || md[KeyCorrelationID] == "" {
			md[KeyCorrelationID] = msgID
		}
		if _, ok := md[KeyTraceID]; !ok || md[KeyTraceID] == "" {
			// 简单生成：使用消息ID或时间戳
			md[KeyTraceID] = msgID
			if md[KeyTraceID] == "" {
				md[KeyTraceID] = time.Now().UTC().Format(time.RFC3339Nano)
			}
		}
		// 对于顶层命令，因果即自身
		if _, ok := md[KeyCausationID]; !ok || md[KeyCausationID] == "" {
			md[KeyCausationID] = msgID
		}
		// 将链路信息放入 Context 以便后续事件沿用
		ctx = context.WithValue(ctx, KeyCorrelationID, md[KeyCorrelationID])
		ctx = context.WithValue(ctx, KeyCausationID, md[KeyCausationID])
		ctx = context.WithValue(ctx, KeyTraceID, md[KeyTraceID])

	case messaging.MessageTypeEvent:
		if _, ok := md[KeyCorrelationID]; !ok || md[KeyCorrelationID] == "" {
			if ctxCorr != "" {
				md[KeyCorrelationID] = ctxCorr
			} else {
				md[KeyCorrelationID] = msgID
			}
		}
		if _, ok := md[KeyCausationID]; !ok || md[KeyCausationID] == "" {
			if ctxCaus != "" {
				md[KeyCausationID] = ctxCaus
			} else {
				md[KeyCausationID] = msgID
			}
		}
		if _, ok := md[KeyTraceID]; !ok || md[KeyTraceID] == "" {
			if ctxTrace != "" {
				md[KeyTraceID] = ctxTrace
			} else {
				md[KeyTraceID] = msgID
			}
		}

	default:
		// 其他类型做最小处理：若缺失 correlation/trace 则兜底为自身ID
		if _, ok := md[KeyCorrelationID]; !ok || md[KeyCorrelationID] == "" {
			md[KeyCorrelationID] = msgID
		}
		if _, ok := md[KeyTraceID]; !ok || md[KeyTraceID] == "" {
			md[KeyTraceID] = msgID
		}
	}

	return next(ctx, message)
}
