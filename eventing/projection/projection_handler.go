package projection

import (
	"context"
	"fmt"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/messaging"
	"gochen/observe/logging"
)

// projectionEventHandler 把投影实例适配为事件总线可订阅的 handler。
type projectionEventHandler[ID comparable] struct {
	runtime *projectionRuntime[ID]
	manager *ProjectionManager[ID]
	unsub   messaging.UnsubscribeFunc
}

// HandleEvent 通过 ProjectionManager 的公共流程处理一条事件，并补充投影侧日志与指标。
func (h *projectionEventHandler[ID]) HandleEvent(ctx context.Context, event eventing.IEvent) error {
	if h == nil || h.runtime == nil || event == nil {
		return nil
	}
	name := ""
	if h.runtime.projection != nil {
		name = h.runtime.projection.Name()
	}

	// 幂等快速检查：若该事件已处理过，直接返回
	if h.runtime.hasProcessedEventID(event.GetID()) {
		return nil
	}

	// 仅当尚未初始化游标或检测到**本投影支持类型**的全局位置空洞时触发追赶。
	// 追加节流：多事件类型系统中全局序号跨类型递增，若只看位置差，几乎每个
	// 在线事件都会命中伪空洞；一次完整追赶即可从持久 checkpoint 重放全部可达
	// 历史，因此窗口内抑制重复追赶不损失恢复能力。
	//
	// 节流窗口的成立前提是“上次追赶已成功覆盖全部可达历史”。两类场景该前提
	// 不成立，必须绕过窗口（bypassThrottle），否则在线游标会单调越进未处理
	// 区间，缺口事件被永久埋没（Resume 也无法恢复：durable checkpoint 落后于
	// 内存游标时不会被采纳）：
	//  1. 已确认的真实空洞（存储查询确认存在未处理的本类型事件）；
	//  2. 上一轮追赶因存储故障失败且尚未成功（catchUpPending）。
	// 节流仅保留给“未确认”的空洞探测（探测失败/存储不可判定），避免存储
	// 异常时每个在线事件都发起一轮注定失败的追赶。
	if h.manager != nil && h.manager.hasCheckpointStore() && h.manager.hasEventStore() {
		cursor := h.runtime.cursorSnapshot()
		needsCatchUp := false
		bypassThrottle := false
		if cursor == nil {
			needsCatchUp = true
		} else if h.runtime.hasPendingCatchUp() {
			// 上一轮追赶失败：必须尽快重试（跳过空洞探测——存储可能仍不可用）。
			needsCatchUp = true
			bypassThrottle = true
		} else if positioned, ok := event.(interface{ GetGlobalPosition() int64 }); ok {
			pos := positioned.GetGlobalPosition()
			if pos > 0 && pos > cursor.Position+1 {
				// 位置差只是必要不充分条件：需确认缺口中有本投影支持的事件。
				realGap, confirmed := h.manager.hasRealGap(ctx, h.runtime, cursor, pos)
				if realGap {
					needsCatchUp = true
					bypassThrottle = confirmed
				}
			}
		}
		if needsCatchUp && !bypassThrottle {
			minInterval := time.Duration(0)
			if h.manager.config != nil {
				minInterval = h.manager.config.CatchUpMinInterval
			}
			if !h.runtime.canStartCatchUp(time.Now(), minInterval) {
				needsCatchUp = false
			}
		}

		if needsCatchUp {
			resumeErr := h.manager.resumeRuntimeFromCheckpoint(ctx, h.runtime, false)
			if resumeErr != nil {
				if errors.Is(resumeErr, errors.NotFound) {
					// checkpoint gap：持久游标引用的历史事件已被清理或不存在，无法重放历史。
					// 降级策略：告警并把内存游标对齐到当前在线事件，继续从在线流处理；
					// 绝不能把无辜的当前事件投入死信，否则后续所有事件都会反复命中同一
					// 故障，造成整个投影死信雪崩与流锁死。
					h.runtime.clearPendingCatchUp()
					h.manager.logger.Warn(ctx, "projection checkpoint gap detected; skip history replay and continue from live event",
						logging.Error(resumeErr),
						logging.String("projection", name),
						logging.String("event_type", event.GetType()),
					)
					h.runtime.alignCursorToEvent(event)
					h.runtime.markCatchUp(time.Now())
				} else {
					// 非 NotFound 通常是 DB/存储瞬断。当前 event 尚未被本处理器处理，
					// 把它当死信会把无辜事件永久抽出主流程；正确做法是返回错误让
					// 上层感知并重试，事件本身不投死信。
					//
					// 状态必须保持可恢复：这里绝不 markError——把状态置为终态 error
					// 会让后续追赶因状态非 running 直接空转、在线事件被 requireRunning
					// 静默跳过（返回 nil，无任何错误信号），投影永久卡死，只能重建。
					// 正确做法是标记“追赶未完成”，下一个事件立即重试追赶（绕过节流
					// 窗口），存储恢复后自动补齐未重放区间（对齐 HEAD 的自愈语义）。
					// 若 replay 内部已把状态置为 error（如毒事件重放失败），恢复 running
					// 以保留自愈能力；错误本身已通过返回值与 LastError 对外暴露。
					h.runtime.markCatchUp(time.Now())
					h.runtime.markCatchUpFailed()
					h.runtime.markRunning()
					h.manager.logger.Error(ctx, "projection failed to catch up from checkpoint; will retry on next event", logging.Error(resumeErr),
						logging.String("projection", name),
						logging.String("event_type", event.GetType()),
					)
					return resumeErr
				}
			} else {
				h.runtime.clearPendingCatchUp()
				h.runtime.markCatchUp(time.Now())
				if h.runtime.hasProcessedEventID(event.GetID()) {
					return nil
				}
			}
		}
	}

	h.runtime.execMu.Lock()
	defer h.runtime.execMu.Unlock()

	// 持锁检查幂等（避免并发重复处理）
	if h.runtime.hasProcessedEventID(event.GetID()) {
		return nil
	}

	// 指标由 applyEventCommon 统一上报（覆盖在线/重放/重建），此处不重复计数。
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

	// 指标已在 applyEventCommon 统一上报（覆盖在线/重放/重建），此处不重复计数。

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
	return errors.NewCode(errors.InvalidInput, "invalid message type").
		WithContext("message_type", fmt.Sprintf("%T", message))
}

func (h *projectionEventHandler[ID]) Type() string {
	return "projectionEventHandler"
}

// 编译期断言：确保 projectionEventHandler 实现 bus.IEventHandler。
var _ bus.IEventHandler = (*projectionEventHandler[int64])(nil)
