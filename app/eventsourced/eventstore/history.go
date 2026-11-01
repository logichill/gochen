package eventstore

import (
	"context"
	"math"
	"time"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/messaging"
)

// EventHistoryEntry 代表一条对用户可读的业务历史记录（单个事件的视图）。
//
// 说明：
//   - SummaryKey/SummaryParams 用于前端或上层根据 key + 参数渲染人类可读文案（支持 i18n）；
//   - ActorID 表示操作人，默认从事件 Metadata 的 operator 提取（兼容旧的 actor_id）；
//   - Initiator/Action/Reason/CorrelationID 对应写入时的审计元数据；
//   - RawPayload 可选携带原始载荷，便于调试或高级展示（可为 nil）。
type EventHistoryEntry[ID comparable] struct {
	EventID       string         `json:"event_id"`
	AggregateID   ID             `json:"aggregate_id"`
	AggregateType string         `json:"aggregate_type"`
	Version       uint64         `json:"version"`
	EventType     string         `json:"event_type"`
	OccurredAt    time.Time      `json:"occurred_at"`
	ActorID       string         `json:"actor_id,omitempty"`
	Initiator     string         `json:"initiator,omitempty"`
	Action        string         `json:"action,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	SummaryKey    string         `json:"summary_key"`
	SummaryParams map[string]any `json:"summary_params,omitempty"`
	RawPayload    any            `json:"raw_payload,omitempty"`
}

// EventHistoryPage 封装分页后的历史记录结果。
type EventHistoryPage[ID comparable] struct {
	Entries  []*EventHistoryEntry[ID] `json:"entries"`
	Total    int                      `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

// EventHistoryMapper 将单个事件映射为业务可读的历史条目。
//
// 返回 nil 表示该事件不需要出现在历史视图中。
type EventHistoryMapper[ID comparable] func(evt eventing.IEvent) *EventHistoryEntry[ID]

func defaultEventHistoryMapper[ID comparable](evt eventing.IEvent) *EventHistoryEntry[ID] {
	if evt == nil {
		return nil
	}
	typed, ok := evt.(eventing.ITypedEvent[ID])
	if !ok {
		return nil
	}
	actor, initiator, action, reason, correlationID := "", "", "", "", ""
	if md := evt.GetMetadata(); md != nil {
		actor, _ = md.GetString(contextx.MetadataOperatorKey)
		if actor == "" {
			actor, _ = md.GetString("actor_id")
		}
		initiator, _ = md.GetString(contextx.MetadataInitiatorKey)
		action, _ = md.GetString(contextx.MetadataActionKey)
		reason, _ = md.GetString(contextx.MetadataReasonKey)
		correlationID, _ = md.GetString(contextx.MetadataCorrelationIDKey)
	}
	payload := messaging.PayloadValue(evt.GetPayload())
	return &EventHistoryEntry[ID]{
		EventID:       evt.GetID(),
		AggregateID:   typed.GetAggregateID(),
		AggregateType: typed.GetAggregateType(),
		Version:       typed.GetVersion(),
		EventType:     evt.GetType(),
		OccurredAt:    evt.GetTimestamp(),
		ActorID:       actor,
		Initiator:     initiator,
		Action:        action,
		Reason:        reason,
		CorrelationID: correlationID,
		SummaryKey:    evt.GetType(),
		SummaryParams: map[string]any{"payload": payload},
		RawPayload:    payload,
	}
}

// loadHistory 加载历史。
func loadHistory[ID comparable](
	ctx context.Context,
	eventStore store.IEventReader[ID],
	aggregateType string,
	id ID,
	afterVersion uint64,
) ([]eventing.IEvent, error) {
	events, err := eventStore.LoadEvents(ctx, aggregateType, id, afterVersion)
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
func EventHistory[ID comparable](
	ctx context.Context,
	eventStore store.IEventReader[ID],
	aggregateType string,
	id ID,
) ([]eventing.IEvent, error) {
	return loadHistory(ctx, eventStore, aggregateType, id, 0)
}

// EventHistoryAfter 查询事件。
//
// 说明：
// - EventHistoryAfter 获取指定版本之后的事件历史。
func EventHistoryAfter[ID comparable](
	ctx context.Context,
	eventStore store.IEventReader[ID],
	aggregateType string,
	id ID,
	afterVersion uint64,
) ([]eventing.IEvent, error) {
	return loadHistory(ctx, eventStore, aggregateType, id, afterVersion)
}

// IEventHistoryStore 表示支持事件历史分页所需的事件存储能力集合。
//
// 说明：
// - EventHistoryPage 需要读取聚合版本号（用于 total）和按版本流式读取事件。
type IEventHistoryStore[ID comparable] interface {
	store.IAggregateEventReader[ID]
}

// EventHistoryPage 查询事件。
//
// 说明：
// - EventHistoryPage 按页获取聚合的事件历史视图：
// - - page 从 1 开始；pageSize > 0
// - - mapper 负责将 IEvent → EventHistoryEntry；若为 nil，则使用默认映射。
func LoadEventHistoryPage[ID comparable](
	ctx context.Context,
	eventStore IEventHistoryStore[ID],
	aggregateType string,
	id ID,
	page int,
	pageSize int,
	mapper EventHistoryMapper[ID],
) (*EventHistoryPage[ID], error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if mapper == nil {
		mapper = defaultEventHistoryMapper[ID]
	}

	// 版本即事件总数（按聚合内版本连续的假设），但需要按 aggregate_type 隔离。
	totalVersion, err := typedAggregateVersion(ctx, eventStore, aggregateType, id)
	if err != nil {
		return nil, err
	}
	if totalVersion > math.MaxInt {
		return nil, errors.NewCode(errors.InvalidInput, "total aggregate version exceeds max int")
	}
	total := int(totalVersion)
	if total == 0 {
		return &EventHistoryPage[ID]{Entries: []*EventHistoryEntry[ID]{}, Total: 0, Page: page, PageSize: pageSize}, nil
	}
	// 溢出保护：与 app/query/pagination.go 的 Validate 保持一致。
	// page-1 与 pageSize 相乘可能整型溢出（page 为调用方可控），回绕后 start
	// 会变成错误值（如 0），导致超大页码静默返回首页数据。
	// 溢出必然意味着请求页远超总页数，按 start >= total 语义返回空页。
	if page-1 > math.MaxInt/pageSize {
		return &EventHistoryPage[ID]{Entries: []*EventHistoryEntry[ID]{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	start := (page - 1) * pageSize
	if start >= total {
		return &EventHistoryPage[ID]{Entries: []*EventHistoryEntry[ID]{}, Total: total, Page: page, PageSize: pageSize}, nil
	}

	entries, err := streamHistoryEntries(ctx, eventStore, aggregateType, id, uint64(start), pageSize, mapper)
	if err != nil {
		return nil, err
	}
	return &EventHistoryPage[ID]{
		Entries:  entries,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// LoadEventHistoryEntries 按偏移量读取聚合的事件历史视图。
//
// 说明：
// - offset 按事件版本计，0 表示从最早事件开始；limit <= 0 时使用默认值 20；
// - mapper 为 nil 时使用默认映射。
func LoadEventHistoryEntries[ID comparable](
	ctx context.Context,
	eventStore IEventHistoryStore[ID],
	aggregateType string,
	id ID,
	offset int,
	limit int,
	mapper EventHistoryMapper[ID],
) ([]*EventHistoryEntry[ID], error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if mapper == nil {
		mapper = defaultEventHistoryMapper[ID]
	}
	totalVersion, err := typedAggregateVersion(ctx, eventStore, aggregateType, id)
	if err != nil {
		return nil, err
	}
	if uint64(offset) >= totalVersion {
		return []*EventHistoryEntry[ID]{}, nil
	}
	return streamHistoryEntries(ctx, eventStore, aggregateType, id, uint64(offset), limit, mapper)
}

func streamHistoryEntries[ID comparable](
	ctx context.Context,
	eventStore IEventHistoryStore[ID],
	aggregateType string,
	id ID,
	afterVersion uint64,
	limit int,
	mapper EventHistoryMapper[ID],
) ([]*EventHistoryEntry[ID], error) {
	streamRes, err := eventStore.StreamAggregate(ctx, &store.AggregateStreamOptions[ID]{
		AggregateType: aggregateType,
		AggregateID:   id,
		AfterVersion:  afterVersion,
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	entries := make([]*EventHistoryEntry[ID], 0, len(streamRes.Events))
	for i := range streamRes.Events {
		e := streamRes.Events[i]
		if entry := mapper(&e); entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func typedAggregateVersion[ID comparable](
	ctx context.Context,
	eventStore IEventHistoryStore[ID],
	aggregateType string,
	id ID,
) (uint64, error) {
	version, err := eventStore.GetAggregateVersion(ctx, aggregateType, id)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}
