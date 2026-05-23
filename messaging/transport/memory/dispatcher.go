// Package memory 实现消息分发逻辑。
package memory

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/logging"
	"gochen/messaging"
	"gochen/messaging/deadletter"
)

// dispatch 分发消息到订阅的处理器。
//
// 说明：
// - 处理流程:
// - 1. 获取精确匹配的处理器。
// - 2. 获取通配符处理器 ("*")
// - 3. 顺序调用所有处理器（同一条消息）。
// - 4. 记录处理结果。
// - 参数:
// - - ctx: 上下文。
// - - message: 待分发的消息。
func (t *MemoryTransport) dispatch(ctx context.Context, message messaging.IMessage) {
	if message == nil {
		return
	}

	messageType := message.GetType()

	// 默认贯通：将 metadata 中的链路信息与 message.Metadata 双向补齐，
	// 避免 transport 直连使用时出现 tenant/trace/operator 断链。
	//
	// 说明（与 contextx.EnsureTraceID 保持一致）：
	// - ctx 优先：同进程内不允许 metadata 覆盖 ctx（避免链路排障混乱）；
	// - metadata 仅用于“ctx 缺失时补齐”（典型：跨进程/异步 Transport 未透传 ctx）；
	// - fallback 兜底：两者都缺失时，用 message.ID 或生成的 trace_id 兜底。
	if ctx == nil {
		// nil ctx 表示“无上游 ctx 可透传”，这里使用兜底 ctx；
		// 同时清空其 trace_id，避免它覆盖 message.Metadata 的链路语义。
		ctx = contextx.Background()
		ctx, _ = contextx.WithTraceID(ctx, "")
	}
	md := message.GetMetadata()

	derived := ctx
	if md != nil {
		if d, err := contextx.DeriveFromMetadata(derived, md); err == nil && d != nil {
			derived = d
		}

		fallback := strings.TrimSpace(message.GetID())
		if fallback == "" {
			fallback = contextx.GenerateTraceID()
		}
		if d, err := contextx.EnsureTraceID(derived, md, fallback); err == nil && d != nil {
			derived = d
		}

		// 双向补齐：metadata 缺失时从 ctx 注入（避免链路字段在同进程内漂移）。
		_ = contextx.InjectTenantID(derived, md)
		_ = contextx.InjectOperator(derived, md)
	}

	t.mutex.RLock()
	// 收集精确匹配和通配符("*")的处理器
	exact := t.handlers[messageType]
	wildcard := t.handlers["*"]

	// 拷贝到新的切片，避免在读锁释放后被并发修改
	combinedLen := len(exact) + len(wildcard)
	handlers := make([]messaging.IMessageHandler, 0, combinedLen)
	if len(exact) > 0 {
		handlers = append(handlers, exact...)
	}
	if len(wildcard) > 0 {
		handlers = append(handlers, wildcard...)
	}
	t.mutex.RUnlock()

	if len(handlers) == 0 {
		return
	}

	// 调用所有注册的处理器
	// 注意：MemoryTransport 是异步分发，handler 错误不会传播给发布者。
	// 如需错误收敛（DLQ），可通过 SetDeadLetterSink 注入 deadletter.ISink。
	for _, handler := range handlers {
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					t.logger.Error(derived, "message handler panicked",
						logging.String("message_type", messageType),
						logging.String("message_id", message.GetID()),
						logging.String("handler", handler.Type()),
						logging.String("panic", fmt.Sprint(r)),
						logging.String("stack", string(debug.Stack())),
					)
					err = errors.NewCode(errors.Internal, "message handler panicked").
						WithContext("panic", fmt.Sprint(r))
				}
			}()
			return handler.Handle(derived, message)
		}()

		if err != nil {
			// 记录错误但继续处理其他处理器
			t.logger.Warn(derived, "message handler failed",
				logging.String("message_type", messageType),
				logging.String("message_id", message.GetID()),
				logging.String("handler", handler.Type()),
				logging.Error(err))

			t.mutex.RLock()
			sink := t.deadLetterSink
			t.mutex.RUnlock()

			if sink != nil {
				if dlqErr := sink.Write(derived, deadletter.Entry{
					Message:     message,
					HandlerType: handler.Type(),
					Err:         err,
					OccurredAt:  time.Now(),
				}); dlqErr != nil {
					t.logger.Error(derived, "write dead letter entry failed",
						logging.String("message_type", messageType),
						logging.String("message_id", message.GetID()),
						logging.String("handler", handler.Type()),
						logging.Error(dlqErr))
				}
			}
		}
	}
}
