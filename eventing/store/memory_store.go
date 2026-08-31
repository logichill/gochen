package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"gochen/errors"
	"gochen/eventing"
	"gochen/messaging"
)

// MemoryEventStore 是支持任意 comparable 聚合 ID 的内存事件存储，仅用于测试与示例。
//
// 说明：
// - 聚合身份由 aggregateType + aggregateID 定义。
type MemoryEventStore[ID comparable] struct {
	mu sync.RWMutex
	// events 按 aggregateType + aggregateID 维度组织。
	events map[memoryAggregateKey[ID]][]eventing.Event[ID]
	// eventByID 按事件 ID 维护全局唯一索引，保持与 SQL store 的 event_id 唯一约束一致。
	eventByID map[string]eventing.Event[ID]
	// globalEvents 按 timestamp/id 维护全局事件流，避免 StreamEvents 每次全量排序。
	globalEvents []eventing.Event[ID]
}

// NewMemoryEventStore 创建一个仅供测试和示例使用的内存事件存储。
func NewMemoryEventStore[ID comparable]() *MemoryEventStore[ID] {
	return &MemoryEventStore[ID]{
		events:    make(map[memoryAggregateKey[ID]][]eventing.Event[ID]),
		eventByID: make(map[string]eventing.Event[ID]),
	}
}

// NewMemoryAggregateStore 返回可作为 IEventStore[ID] 使用的内存事件存储。
func NewMemoryAggregateStore[ID comparable]() *MemoryEventStore[ID] {
	return NewMemoryEventStore[ID]()
}

// NewMemoryEventStreamStore 返回可作为 IEventStreamStore[ID] 使用的内存事件存储。
func NewMemoryEventStreamStore[ID comparable]() *MemoryEventStore[ID] {
	return NewMemoryEventStore[ID]()
}

// AppendEvents 以乐观锁语义向指定聚合追加一批事件。
func (m *MemoryEventStore[ID]) AppendEvents(ctx context.Context, aggregateType string, aggregateID ID, events []eventing.IStorableEvent[ID], expectedVersion uint64) error {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return err
	}
	if m == nil {
		return errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	if len(events) == 0 {
		return nil
	}

	// 性能优化：预先在锁外进行验证和类型转换，减少临界区范围
	// 这些操作不依赖共享状态，可以安全地在锁外执行
	convertedEvents := make([]eventing.Event[ID], len(events))

	for i, e := range events {
		if isNilMemoryEvent(e) {
			return errors.NewCode(errors.InvalidInput, "event cannot be nil").WithContext("event_index", i)
		}
		if e.GetAggregateID() != aggregateID {
			return errors.NewCode(errors.InvalidInput, "event aggregate id does not match append aggregate id").
				WithContext("aggregate_id", aggregateID).
				WithContext("event_aggregate_id", e.GetAggregateID()).
				WithContext("event_id", e.GetID()).
				WithContext("event_type", e.GetType())
		}
		eventAggregateType := strings.TrimSpace(e.GetAggregateType())
		if eventAggregateType != "" && eventAggregateType != aggregateType {
			return errors.NewCode(errors.InvalidInput, "event aggregate type does not match append aggregate type").
				WithContext("event_id", e.GetID()).
				WithContext("event_type", e.GetType())
		}

		// 类型转换（锁外）
		event, ok := e.(*eventing.Event[ID])
		if !ok {
			return errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported event type: %T, expected *eventing.Event", e))
		}

		// 版本校验（锁外）
		expectedEventVersion := expectedVersion + uint64(i) + 1
		if event.GetVersion() != expectedEventVersion {
			return errors.NewCode(errors.InvalidInput, fmt.Sprintf("event version not sequential: expected %d, got %d", expectedEventVersion, event.GetVersion()))
		}

		convertedEvents[i] = cloneMemoryEvent(*event)
		// 在克隆副本上设置 aggregateType，避免修改调用方传入的事件对象。
		convertedEvents[i].SetAggregateType(aggregateType)
		// 验证克隆副本（锁外），避免为补齐 aggregate type 修改调用方传入的事件对象。
		if err := convertedEvents[i].Validate(); err != nil {
			return err
		}
	}

	// 性能优化：缩小锁范围，只在真正修改共享状态时持锁
	// 锁持有时间从 O(n*验证时间) 降低到 O(n*append操作)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.initializeUnsafe()

	// 版本检查（必须在锁内，确保原子性）
	key := memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID}
	currentVersion, err := m.getAggregateVersionUnsafe(key)
	if err != nil {
		return err
	}
	if currentVersion != expectedVersion {
		if m.isDuplicateAppendUnsafe(key, convertedEvents, expectedVersion) {
			return nil
		}
		if err := m.validateEventIDUniquenessUnsafe(aggregateID, convertedEvents); err != nil {
			return err
		}
		return errors.NewCode(errors.Concurrency,
			fmt.Sprintf("concurrency conflict: aggregate=%v, expected=%d, actual=%d", aggregateID, expectedVersion, currentVersion),
		).WithContext("aggregate_id", aggregateID).
			WithContext("expected_version", expectedVersion).
			WithContext("actual_version", currentVersion)
	}
	if err := m.validateEventIDUniquenessUnsafe(aggregateID, convertedEvents); err != nil {
		return err
	}

	// 初始化存储（如果需要）
	if m.events[key] == nil {
		m.events[key] = make([]eventing.Event[ID], 0, len(convertedEvents))
	}
	// 写入（convertedEvents 已在锁外完成深拷贝，无需二次克隆）
	m.events[key] = append(m.events[key], convertedEvents...)
	for _, evt := range convertedEvents {
		m.eventByID[evt.GetID()] = evt
	}
	m.insertGlobalEventsUnsafe(convertedEvents)

	return nil
}

// LoadEvents 返回指定聚合类型下、某个版本之后的事件。
func (m *MemoryEventStore[ID]) LoadEvents(ctx context.Context, aggregateType string, aggregateID ID, afterVersion uint64) ([]eventing.Event[ID], error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID}
	aggregateEvents := m.events[key]
	if len(aggregateEvents) == 0 {
		return []eventing.Event[ID]{}, nil
	}
	startIdx := searchMemoryEventsAfterVersion(aggregateEvents, afterVersion)
	return cloneMemoryEvents(aggregateEvents[startIdx:]), nil
}

// StreamAggregate 按版本顺序分页读取单个聚合的事件流。
func (m *MemoryEventStore[ID]) StreamAggregate(ctx context.Context, opts *AggregateStreamOptions[ID]) (*AggregateStreamResult[ID], error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	if opts == nil {
		return nil, errors.NewCode(errors.InvalidInput, "AggregateStreamOptions cannot be nil")
	}
	if invalidMemoryAggregateID(opts.AggregateID) {
		return nil, errors.NewCode(errors.InvalidInput, "invalid aggregate id").WithContext("aggregate_id", opts.AggregateID)
	}
	aggregateType := strings.TrimSpace(opts.AggregateType)
	if aggregateType == "" {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	limit := NormalizeStreamLimit(opts.Limit)

	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: opts.AggregateID}
	aggregateEvents := m.events[key]
	if len(aggregateEvents) == 0 {
		return &AggregateStreamResult[ID]{Events: []eventing.Event[ID]{}}, nil
	}

	res := make([]eventing.Event[ID], 0, limit)
	var nextVersion uint64
	for _, e := range aggregateEvents {
		if e.GetVersion() <= opts.AfterVersion {
			continue
		}
		res = append(res, cloneMemoryEvent(e))
		nextVersion = e.GetVersion()
		if len(res) >= limit {
			break
		}
	}

	result := &AggregateStreamResult[ID]{Events: res, NextVersion: nextVersion}
	// HasMore：若还有事件版本大于 nextVersion，则标记为 true
	if nextVersion > 0 {
		for _, e := range aggregateEvents {
			if e.GetVersion() > nextVersion {
				result.HasMore = true
				break
			}
		}
	}
	return result, nil
}

// StreamEvents 对全局事件集应用过滤和游标分页。
func (m *MemoryEventStore[ID]) StreamEvents(ctx context.Context, opts *StreamOptions) (*StreamResult[ID], error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	m.mu.RLock()
	if err := ctx.Err(); err != nil {
		m.mu.RUnlock()
		return nil, err
	}
	if opts != nil && opts.After != "" {
		if _, exists := m.eventByID[opts.After]; !exists {
			m.mu.RUnlock()
			return nil, errors.NewCode(errors.NotFound, "cursor not found").WithContext("cursor", opts.After)
		}
	}
	all := append([]eventing.Event[ID](nil), m.globalEvents...)
	m.mu.RUnlock()

	result := FilterOrderedEventsWithOptions[ID](all, opts)
	result.Events = cloneMemoryEvents(result.Events)
	return result, nil
}

func isNilMemoryEvent[ID comparable](event eventing.IStorableEvent[ID]) bool {
	if event == nil {
		return true
	}
	value := reflect.ValueOf(event)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// HasAggregate 通过是否已存在事件流判断聚合是否存在。
func (m *MemoryEventStore[ID]) HasAggregate(ctx context.Context, aggregateType string, aggregateID ID) (bool, error) {
	version, err := m.GetAggregateVersion(ctx, aggregateType, aggregateID)
	return version > 0, err
}

// GetAggregateVersion 返回指定聚合类型和 ID 的当前版本号。
func (m *MemoryEventStore[ID]) GetAggregateVersion(ctx context.Context, aggregateType string, aggregateID ID) (uint64, error) {
	if err := memoryEventStoreContextError(ctx); err != nil {
		return 0, err
	}
	if m == nil {
		return 0, errors.NewCode(errors.InvalidInput, "memory event store is nil")
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType == "" {
		return 0, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return m.getAggregateVersionUnsafe(memoryAggregateKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID})
}

func (m *MemoryEventStore[ID]) initializeUnsafe() {
	if m.events == nil {
		m.events = make(map[memoryAggregateKey[ID]][]eventing.Event[ID])
	}
	if m.eventByID == nil {
		m.eventByID = make(map[string]eventing.Event[ID])
	}
}

func memoryEventStoreContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

type memoryAggregateKey[ID comparable] struct {
	aggregateType string
	aggregateID   ID
}

// getAggregateVersionUnsafe 在持锁前提下读取某个复合键对应的最新版本号。
func (m *MemoryEventStore[ID]) getAggregateVersionUnsafe(key memoryAggregateKey[ID]) (uint64, error) {
	aggregateEvents, exists := m.events[key]
	if !exists || len(aggregateEvents) == 0 {
		return 0, nil
	}
	return aggregateEvents[len(aggregateEvents)-1].GetVersion(), nil
}

func (m *MemoryEventStore[ID]) validateEventIDUniquenessUnsafe(aggregateID ID, events []eventing.Event[ID]) error {
	seen := make(map[string]eventing.Event[ID], len(events))
	for _, evt := range events {
		if _, ok := m.eventByID[evt.GetID()]; ok {
			return duplicateMemoryEventIDError(aggregateID, evt)
		}
		if _, ok := seen[evt.GetID()]; ok {
			return duplicateMemoryEventIDError(aggregateID, evt)
		}
		seen[evt.GetID()] = evt
	}
	return nil
}

func duplicateMemoryEventIDError[ID comparable](aggregateID ID, evt eventing.Event[ID]) error {
	return errors.NewCode(errors.Duplicate, fmt.Sprintf("event %s already exists", evt.GetID())).
		WithContext("event_id", evt.GetID()).
		WithContext("event_type", evt.GetType()).
		WithContext("aggregate_id", aggregateID).
		WithContext("version", evt.GetVersion())
}

func (m *MemoryEventStore[ID]) insertGlobalEventsUnsafe(events []eventing.Event[ID]) {
	if len(events) == 0 {
		return
	}
	incoming := append([]eventing.Event[ID](nil), events...)
	sort.Slice(incoming, func(i, j int) bool {
		return eventBefore(incoming[i], incoming[j])
	})
	if len(m.globalEvents) == 0 || !eventBefore(incoming[0], m.globalEvents[len(m.globalEvents)-1]) {
		m.globalEvents = append(m.globalEvents, incoming...)
		return
	}

	merged := make([]eventing.Event[ID], 0, len(m.globalEvents)+len(incoming))
	i, j := 0, 0
	for i < len(m.globalEvents) && j < len(incoming) {
		if eventBefore(incoming[j], m.globalEvents[i]) {
			merged = append(merged, incoming[j])
			j++
		} else {
			merged = append(merged, m.globalEvents[i])
			i++
		}
	}
	merged = append(merged, m.globalEvents[i:]...)
	merged = append(merged, incoming[j:]...)
	m.globalEvents = merged
}

func (m *MemoryEventStore[ID]) isDuplicateAppendUnsafe(key memoryAggregateKey[ID], events []eventing.Event[ID], expectedVersion uint64) bool {
	aggregateEvents := m.events[key]
	if len(events) == 0 || expectedVersion > uint64(len(aggregateEvents)) {
		return false
	}
	start := int(expectedVersion)
	if start+len(events) > len(aggregateEvents) {
		return false
	}
	lastVersion := aggregateEvents[len(aggregateEvents)-1].GetVersion()
	if lastVersion != expectedVersion+uint64(len(events)) {
		return false
	}
	for i := range events {
		if !sameMemoryEvent(aggregateEvents[start+i], events[i]) {
			return false
		}
	}
	return true
}

func sameMemoryEvent[ID comparable](a eventing.Event[ID], b eventing.Event[ID]) bool {
	return a.GetID() == b.GetID() &&
		a.GetKind() == b.GetKind() &&
		a.GetType() == b.GetType() &&
		a.GetTimestamp().Equal(b.GetTimestamp()) &&
		a.GetAggregateID() == b.GetAggregateID() &&
		a.GetAggregateType() == b.GetAggregateType() &&
		a.GetVersion() == b.GetVersion() &&
		a.EventSchemaVersion() == b.EventSchemaVersion() &&
		sameMemoryPayload(a.GetPayload(), b.GetPayload()) &&
		sameMemoryMetadata(a.GetMetadata(), b.GetMetadata())
}

func sameMemoryPayload(a messaging.Payload, b messaging.Payload) bool {
	left, leftOK := memoryPayloadBytes(a)
	right, rightOK := memoryPayloadBytes(b)
	return leftOK && rightOK && bytes.Equal(left, right)
}

func memoryPayloadBytes(payload messaging.Payload) ([]byte, bool) {
	data, err := json.Marshal(messaging.PayloadValue(payload))
	if err != nil {
		return nil, false
	}
	return data, true
}

func sameMemoryMetadata(a *messaging.Metadata, b *messaging.Metadata) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left := a.MapCopy()
	right := b.MapCopy()
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if rightValue, ok := right[key]; !ok || rightValue != leftValue {
			return false
		}
	}
	return true
}

func searchMemoryEventsAfterVersion[ID comparable](events []eventing.Event[ID], afterVersion uint64) int {
	return sort.Search(len(events), func(i int) bool {
		return events[i].GetVersion() > afterVersion
	})
}

func cloneMemoryEvents[ID comparable](events []eventing.Event[ID]) []eventing.Event[ID] {
	if len(events) == 0 {
		return []eventing.Event[ID]{}
	}
	out := make([]eventing.Event[ID], len(events))
	for i := range events {
		out[i] = cloneMemoryEvent(events[i])
	}
	return out
}

func cloneMemoryEvent[ID comparable](evt eventing.Event[ID]) eventing.Event[ID] {
	clone := evt
	clone.Payload = messaging.NewPayload(messaging.CloneValue(messaging.PayloadValue(evt.GetPayload())))
	if evt.Metadata != nil {
		clone.Metadata = messaging.NewMetadata()
		for k, v := range evt.Metadata.MapCopy() {
			clone.Metadata.Set(k, v)
		}
	}
	return clone
}

func invalidMemoryAggregateID[ID comparable](id ID) bool {
	value := reflect.ValueOf(id)
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() <= 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.String:
		return strings.TrimSpace(value.String()) == ""
	default:
		var zero ID
		return id == zero
	}
}

// 编译期断言：确保泛型内存存储实现事件流接口。
var _ IEventStreamStore[int64] = (*MemoryEventStore[int64])(nil)
var _ IEventStreamStore[string] = (*MemoryEventStore[string])(nil)
