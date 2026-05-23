package eventsourced

import (
	"context"
	"time"

	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

// EventHistoryEntry 代表一条对用户可读的业务历史记录（单个事件的视图）。
//
// 说明：
//   - SummaryKey/SummaryParams 用于前端或上层根据 key + 参数渲染人类可读文案（支持 i18n）；
//   - ActorID 通常从事件 Metadata 中提取（例如 "actor_id"），由领域层的 mapper 决定；
//   - RawPayload 可选携带原始载荷，便于调试或高级展示（可为 nil）。
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

// EventHistoryPage 封装分页后的历史记录结果。
type EventHistoryPage struct {
	Entries  []*EventHistoryEntry `json:"entries"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// EventHistoryMapper 将单个事件映射为业务可读的历史条目。
//
// 返回 nil 表示该事件不需要出现在历史视图中。
type EventHistoryMapper func(evt eventing.IEvent) *EventHistoryEntry

func defaultEventHistoryMapper(evt eventing.IEvent) *EventHistoryEntry {
	if evt == nil {
		return nil
	}
	typed, ok := evt.(eventing.ITypedEvent[int64])
	if !ok {
		return nil
	}
	actor := ""
	if md := evt.GetMetadata(); md != nil {
		if v, ok := md.GetString("actor_id"); ok {
			actor = v
		}
	}
	payload := messaging.PayloadValue(evt.GetPayload())
	return &EventHistoryEntry{
		EventID:       evt.GetID(),
		AggregateID:   typed.GetAggregateID(),
		AggregateType: typed.GetAggregateType(),
		Version:       typed.GetVersion(),
		EventType:     evt.GetType(),
		OccurredAt:    evt.GetTimestamp(),
		ActorID:       actor,
		SummaryKey:    evt.GetType(),
		SummaryParams: map[string]any{"payload": payload},
		RawPayload:    payload,
	}
}

// loadHistory 加载历史。
func loadHistory(
	ctx context.Context,
	eventStore store.IEventStore[int64],
	aggregateType string,
	id int64,
	afterVersion uint64,
) ([]eventing.IEvent, error) {
	var (
		events []eventing.Event[int64]
		err    error
	)
	events, err = eventStore.LoadEventsByType(ctx, aggregateType, id, afterVersion)
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

// EventHistory 查询事件。
//
// 说明：
// - EventHistory 获取聚合的事件历史。
//
// 参数：
// - eventStore：事件存储实现。
func EventHistory(
	ctx context.Context,
	eventStore store.IEventStore[int64],
	aggregateType string,
	id int64,
) ([]eventing.IEvent, error) {
	return loadHistory(ctx, eventStore, aggregateType, id, 0)
}

// EventHistoryAfter 查询事件。
//
// 说明：
// - EventHistoryAfter 获取指定版本之后的事件历史。
//
// 参数：
// - eventStore：事件存储实现。
func EventHistoryAfter(
	ctx context.Context,
	eventStore store.IEventStore[int64],
	aggregateType string,
	id int64,
	afterVersion uint64,
) ([]eventing.IEvent, error) {
	return loadHistory(ctx, eventStore, aggregateType, id, afterVersion)
}

// IEventHistoryStore 表示支持事件历史分页所需的事件存储能力集合。
//
// 说明：
// - EventHistoryPage 需要读取聚合版本号（用于 total）和按版本流式读取事件；
// - 当前仅支持 ID=int64 的事件历史视图（与 EventHistoryEntry 的聚合 ID 类型一致）。
type IEventHistoryStore interface {
	store.IEventStreamStore[int64]
}

// EventHistoryPage 查询事件。
//
// 说明：
// - EventHistoryPage 按页获取聚合的事件历史视图：
// - - page 从 1 开始；pageSize > 0
// - - mapper 负责将 IEvent → EventHistoryEntry；若为 nil，则使用默认映射。
func LoadEventHistoryPage(
	ctx context.Context,
	eventStore IEventHistoryStore,
	aggregateType string,
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

	// 版本即事件总数（按聚合内版本连续的假设），但需要按 aggregate_type 隔离。
	totalVersion, err := typedAggregateVersion(ctx, eventStore, aggregateType, id)
	if err != nil {
		return nil, err
	}
	total := int(totalVersion)
	if total == 0 {
		return &EventHistoryPage{Entries: []*EventHistoryEntry{}, Total: 0, Page: page, PageSize: pageSize}, nil
	}
	start := (page - 1) * pageSize
	if start >= total {
		return &EventHistoryPage{Entries: []*EventHistoryEntry{}, Total: total, Page: page, PageSize: pageSize}, nil
	}

	opts := &store.AggregateStreamOptions[int64]{
		AggregateType: aggregateType,
		AggregateID:   id,
		AfterVersion:  uint64(start),
		Limit:         pageSize,
	}
	streamRes, err := eventStore.StreamAggregate(ctx, opts)
	if err != nil {
		return nil, err
	}

	entries := make([]*EventHistoryEntry, 0, len(streamRes.Events))
	for i := range streamRes.Events {
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

func typedAggregateVersion(
	ctx context.Context,
	eventStore IEventHistoryStore,
	aggregateType string,
	id int64,
) (uint64, error) {
	events, err := eventStore.LoadEventsByType(ctx, aggregateType, id, 0)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].GetVersion(), nil
}
