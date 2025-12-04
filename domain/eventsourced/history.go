package eventsourced

import (
	"context"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
)

// EventHistoryEntry 代表一条对用户可读的业务历史记录（单个事件的视图）
//
// 说明：
// - SummaryKey/SummaryParams 用于前端或上层根据 key + 参数渲染人类可读文案（支持 i18n）；
// - ActorID 通常从事件 Metadata 中提取（例如 "actor_id"），由领域层的 mapper 决定；
// - RawPayload 可选携带原始载荷，便于调试或高级展示（可为 nil）。
type EventHistoryEntry struct {
	EventID       string         `json:"event_id"`
	AggregateID   int64          `json:"aggregate_id"`
	AggregateType string         `json:"aggregate_type"`
	Version       uint64         `json:"version"`
	EventType     string         `json:"event_type"`
	OccurredAt    time.Time      `json:"occurred_at"`
	ActorID       string         `json:"actor_id,omitempty"`
	SummaryKey    string         `json:"summary_key"`
	SummaryParams map[string]any `json:"summary_params,omitempty"`
	RawPayload    any            `json:"raw_payload,omitempty"`
}

// EventHistoryPage 封装分页后的历史记录结果
type EventHistoryPage struct {
	Entries  []*EventHistoryEntry `json:"entries"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// EventHistoryMapper 将单个事件映射为业务可读的历史条目
//
// 返回 nil 表示该事件不需要出现在历史视图中。
type EventHistoryMapper func(evt eventing.IEvent) *EventHistoryEntry

// defaultEventHistoryMapper 是一个退化但可用的默认实现：
// - SummaryKey 使用事件类型
// - SummaryParams 直接塞入 Payload（如果是简单类型/struct）
// - ActorID 尝试从 metadata["actor_id"] 中读取
func defaultEventHistoryMapper(evt eventing.IEvent) *EventHistoryEntry {
	if evt == nil {
		return nil
	}
	base, ok := evt.(*eventing.Event)
	if !ok {
		return nil
	}
	actor := ""
	if md := base.GetMetadata(); md != nil {
		if v, ok := md["actor_id"].(string); ok {
			actor = v
		}
	}
	return &EventHistoryEntry{
		EventID:       base.GetID(),
		AggregateID:   base.GetAggregateID(),
		AggregateType: base.GetAggregateType(),
		Version:       base.GetVersion(),
		EventType:     base.GetType(),
		OccurredAt:    base.GetTimestamp(),
		ActorID:       actor,
		SummaryKey:    base.GetType(),
		SummaryParams: map[string]any{"payload": base.GetPayload()},
		RawPayload:    base.GetPayload(),
	}
}

// GetEventHistory 获取聚合的事件历史
func (r *EventSourcedRepository[T]) GetEventHistory(ctx context.Context, id int64) ([]eventing.IEvent, error) {
	var (
		events []eventing.Event
		err    error
	)
	if typedStore, ok := r.eventStore.(store.ITypedEventStore); ok {
		events, err = typedStore.LoadEventsByType(ctx, r.aggregateType, id, 0)
	} else {
		events, err = r.eventStore.LoadEvents(ctx, id, 0)
	}
	if err != nil {
		return nil, err
	}
	result := make([]eventing.IEvent, len(events))
	for i := range events {
		e := events[i]
		result[i] = &e
	}
	return result, nil
}

// GetEventHistoryAfter 获取指定版本之后的事件历史
func (r *EventSourcedRepository[T]) GetEventHistoryAfter(ctx context.Context, id int64, afterVersion uint64) ([]eventing.IEvent, error) {
	var (
		events []eventing.Event
		err    error
	)
	if typedStore, ok := r.eventStore.(store.ITypedEventStore); ok {
		events, err = typedStore.LoadEventsByType(ctx, r.aggregateType, id, afterVersion)
	} else {
		events, err = r.eventStore.LoadEvents(ctx, id, afterVersion)
	}
	if err != nil {
		return nil, err
	}
	result := make([]eventing.IEvent, len(events))
	for i := range events {
		e := events[i]
		result[i] = &e
	}
	return result, nil
}

// GetEventHistoryPage 按页获取聚合的事件历史视图：
// - page 从 1 开始；pageSize > 0
// - mapper 负责将 IEvent → EventHistoryEntry；若为 nil，则使用默认映射
// - 当前实现优先使用 IAggregateEventStore + IAggregateInspector，其次回退为内存分页
func (r *EventSourcedRepository[T]) GetEventHistoryPage(
	ctx context.Context,
	id int64,
	page int,
	pageSize int,
	mapper EventHistoryMapper,
) (*EventHistoryPage, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if mapper == nil {
		mapper = defaultEventHistoryMapper
	}

	// 优先使用 IAggregateEventStore + IAggregateInspector 做基于版本的分页
	if aggStore, ok := r.eventStore.(store.IAggregateEventStore); ok {
		if inspector, ok2 := r.eventStore.(store.IAggregateInspector); ok2 {
			// 版本即事件总数（按聚合内版本连续的假设）
			totalVersion, err := inspector.GetAggregateVersion(ctx, id)
			if err != nil {
				return nil, err
			}
			total := int(totalVersion)
			if total == 0 {
				return &EventHistoryPage{Entries: []*EventHistoryEntry{}, Total: 0, Page: page, PageSize: pageSize}, nil
			}
			start := (page - 1) * pageSize
			if start >= total {
				// 页码越界，返回空列表但保留总数
				return &EventHistoryPage{Entries: []*EventHistoryEntry{}, Total: total, Page: page, PageSize: pageSize}, nil
			}

			opts := &store.AggregateStreamOptions{
				AggregateType: r.aggregateType,
				AggregateID:   id,
				AfterVersion:  uint64(start),
				Limit:         pageSize,
			}
			streamRes, err := aggStore.StreamAggregate(ctx, opts)
			if err != nil {
				return nil, err
			}

			entries := make([]*EventHistoryEntry, 0, len(streamRes.Events))
			for i := range streamRes.Events {
				// 需要传地址给 mapper（接口类型）
				e := streamRes.Events[i]
				if entry := mapper(&e); entry != nil {
					entries = append(entries, entry)
				}
			}

			return &EventHistoryPage{
				Entries:  entries,
				Total:    total,
				Page:     page,
				PageSize: pageSize,
			}, nil
		}
	}

	// 回退方案：一次性加载该聚合全部事件，再内存映射 + 分页
	events, err := r.GetEventHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	allEntries := make([]*EventHistoryEntry, 0, len(events))
	for _, e := range events {
		if entry := mapper(e); entry != nil {
			allEntries = append(allEntries, entry)
		}
	}
	total := len(allEntries)
	if total == 0 {
		return &EventHistoryPage{Entries: []*EventHistoryEntry{}, Total: 0, Page: page, PageSize: pageSize}, nil
	}
	start := (page - 1) * pageSize
	if start >= total {
		return &EventHistoryPage{Entries: []*EventHistoryEntry{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	pageEntries := allEntries[start:end]
	return &EventHistoryPage{Entries: pageEntries, Total: total, Page: page, PageSize: pageSize}, nil
}

type snapshotAggregateAdapter[T IEventSourcedAggregate[int64]] struct {
	aggregate T
}

func (a snapshotAggregateAdapter[T]) GetID() int64 {
	return a.aggregate.GetID()
}

func (a snapshotAggregateAdapter[T]) GetVersion() uint64 {
	version := a.aggregate.GetVersion()
	if version <= 0 {
		return 0
	}
	return uint64(version)
}

func (a snapshotAggregateAdapter[T]) GetAggregateType() string {
	return a.aggregate.GetAggregateType()
}
