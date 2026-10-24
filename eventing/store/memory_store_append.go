package store

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"gochen/errors"
	"gochen/eventing"
)

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
