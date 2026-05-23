package projection

import (
	"context"
	"fmt"
	"time"

	gerrors "gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/logging"
	"gochen/messaging"
)

// projectionEventHandler 把投影实例适配为事件总线可订阅的 handler。
type projectionEventHandler[ID comparable] struct {
	runtime *projectionRuntime[ID]
	manager *ProjectionManager[ID]
	unsub   messaging.UnsubscribeFunc
}

// HandleEvent 通过 ProjectionManager 的公共流程处理一条事件，并补充投影侧日志与指标。
func (h *projectionEventHandler[ID]) HandleEvent(ctx context.Context, event eventing.IEvent) error {
	if h == nil || h.runtime == nil {
		return nil
	}
	name := h.runtime.projection.Name()
	if h.manager != nil && h.manager.hasCheckpointStore() {
		resumeErr := h.manager.resumeRuntimeFromCheckpoint(ctx, h.runtime, false)
		if resumeErr != nil {
			h.manager.config.DeadLetterFunc(resumeErr, event, name)
			h.manager.logger.Error(ctx, "projection failed to catch up from checkpoint", logging.Error(resumeErr),
				logging.String("projection", name),
				logging.String("event_type", event.GetType()),
			)
		}
		return resumeErr
	}

	h.runtime.execMu.Lock()
	defer h.runtime.execMu.Unlock()

	metrics := h.manager.getMetrics()

	res, err := h.manager.applyEventCommon(ctx, h.runtime, event, applyEventCommonOptions{
		requireRunning:          true,
		enableRetry:             false,
		allowCheckpoint:         true,
		clearLastErrorOnSuccess: false,
		upgradeLogMessage:       "projection event payload upgrade/hydrate failed",
	})
	if res.skipped {
		return nil
	}

	lag := time.Since(event.GetTimestamp())
	if lag < 0 {
		lag = 0
	}
	if metrics != nil {
		metrics.RecordEventProcessed(res.handleDuration, err == nil)
		metrics.RecordProjectionUpdate(err == nil, lag)
	}

	if err != nil {
		// 死信回调：不对事件 ID 类型做假设（ProjectionConfig.DeadLetterFunc 接受 eventing.IEvent）。
		h.manager.config.DeadLetterFunc(err, event, name)

		// 使用快照后的计数值进行日志记录，避免在无锁状态下访问共享状态
		h.manager.logger.Error(ctx, "projection failed to handle event", logging.Error(err),
			logging.String("projection", name),
			logging.String("event_type", event.GetType()),
			logging.Int64("processed_events", res.processedEvents),
			logging.Int64("failed_events", res.failedEvents),
		)
		return err
	}

	h.manager.logger.Debug(ctx, "projection handled event successfully",
		logging.String("event_type", event.GetType()),
		logging.String("projection", name),
	)
	return nil
}

func (h *projectionEventHandler[ID]) EventTypes() []string {
	if h == nil || h.runtime == nil || h.runtime.projection == nil {
		return nil
	}
	return h.runtime.projection.SupportedEventTypes()
}

func (h *projectionEventHandler[ID]) HandlerName() string {
	if h == nil || h.runtime == nil || h.runtime.projection == nil {
		return ""
	}
	return h.runtime.projection.Name()
}

// Handle 把总线消息校验为事件后转发给 HandleEvent。
func (h *projectionEventHandler[ID]) Handle(ctx context.Context, message messaging.IMessage) error {
	// 尝试将message转换为 eventing.IEvent
	if event, ok := message.(eventing.IEvent); ok {
		return h.HandleEvent(ctx, event)
	}
	return gerrors.NewCode(gerrors.InvalidInput, "invalid message type").
		WithContext("message_type", fmt.Sprintf("%T", message))
}

func (h *projectionEventHandler[ID]) Type() string {
	return "projectionEventHandler"
}

// 编译期断言：确保 projectionEventHandler 实现 bus.IEventHandler。
var _ bus.IEventHandler = (*projectionEventHandler[int64])(nil)
