// Package messaging 提供消息传输层抽象。
package messaging

import (
	"context"
	stderrors "errors"
	"reflect"

	"gochen/errors"
)

// ErrTransportAlreadyStopped 标识 transport 已处于停止态，供 shutdown 清理路径做幂等判定。
var ErrTransportAlreadyStopped = stderrors.New("transport already stopped")

// UnsubscribeFunc 表示一个订阅的取消函数。
//
// 约定：
// - 返回 nil 表示已成功取消或已处于取消状态（幂等）；具体幂等性由实现决定；
// - ctx 可用于远程 transport 的超时/取消控制；对于纯内存实现可忽略。
type UnsubscribeFunc func(ctx context.Context) error

// ITransport 消息传输接口。
//
// 语义约定：
//   - Publish/PublishAll 返回的 error 只代表“传输层本身”的错误（连接失败、队列已满、未 Start 等）；
//   - 对于异步实现（如 memory/redisstreams/natsjetstream），消息处理器（IMessageHandler.Handle）的错误通常不会通过返回值暴露，
//     而是由实现自行记录日志或上报监控；
//   - 对于同步实现（如 transport/sync），Publish/PublishAll 可能会在同一调用中直接执行所有处理器，并将其错误聚合到返回值中。
//
// 调用方应将非 nil error 视为“消息未成功交给传输层”的信号；业务级错误建议通过消息载荷或领域层约定返回，而不是依赖 Transport 的 error。
type ITransport interface {
	Publish(ctx context.Context, message IMessage) error
	PublishAll(ctx context.Context, messages []IMessage) error
	Subscribe(ctx context.Context, messageType string, handler IMessageHandler) (UnsubscribeFunc, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Stats() TransportStats
}

// ITransportStopSnapshot 抽象传输停止时返回待处理消息快照的可选能力接口。
type ITransportStopSnapshot interface {
	StopWithSnapshot(ctx context.Context) (pending []IMessage, err error)
}

// NewTransportAlreadyStoppedError 创建带标准哨兵 cause 的已停止错误。
func NewTransportAlreadyStoppedError(message string) error {
	return errors.NewCodeWithCause(errors.Conflict, message, ErrTransportAlreadyStopped)
}

// TransportAlreadyStopped 判断 Stop/StopWithSnapshot 返回的错误是否明确表示传输层已处于停止态。
func TransportAlreadyStopped(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrTransportAlreadyStopped)
}

// StopTransport 使用统一生命周期语义停止 transport，并把明确的“已停止”视为幂等成功。
//
// 说明：ctx 为 nil 时按 context.Background() 处理，便于清理入口在无请求上下文时安全调用。
func StopTransport(ctx context.Context, transport interface{ Stop(context.Context) error }) error {
	if transport == nil || transportTypedNil(transport) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot, ok := transport.(ITransportStopSnapshot); ok {
		_, err := snapshot.StopWithSnapshot(ctx)
		if TransportAlreadyStopped(err) {
			return nil
		}
		return err
	}
	err := transport.Stop(ctx)
	if TransportAlreadyStopped(err) {
		return nil
	}
	return err
}

func transportTypedNil(transport interface{ Stop(context.Context) error }) bool {
	v := reflect.ValueOf(transport)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// ISynchronousTransport 抽象Synchronous传输能力接口。
type ISynchronousTransport interface {
	IsSynchronous() bool
}

// TransportStats 传输层统计信息。
type TransportStats struct {
	Running      bool     `json:"running"`
	HandlerCount int      `json:"handler_count"`
	MessageTypes []string `json:"message_types"`
	QueueSize    int      `json:"queue_size,omitempty"`
	QueueDepth   int      `json:"queue_depth,omitempty"`
	WorkerCount  int      `json:"worker_count,omitempty"`
}
