package eventsourced

import (
	"context"
	"errors"
	"testing"
	"time"

	"gochen/domain/entity"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/messaging"
)

// TestAggregate 测试聚合
type TestAggregate struct {
	*entity.EventSourcedAggregate[int64]
	Value int
}

func NewTestAggregate(id int64) *TestAggregate {
	return &TestAggregate{
		EventSourcedAggregate: entity.NewEventSourcedAggregate(id, "TestAggregate"),
		Value:                 0,
	}
}

func (a *TestAggregate) ApplyEvent(evt eventing.IEvent) error {
	switch evt.GetType() {
	case "ValueSet":
		if payload, ok := evt.GetPayload().(int); ok {
			a.Value = payload
		}
	case "ValueIncremented":
		if payload, ok := evt.GetPayload().(int); ok {
			a.Value += payload
		}
	}
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

// MockEventStore 模拟事件存储
type MockEventStore struct {
	events      map[int64][]eventing.Event
	appendCalls int
	loadCalls   int
	appendError error
	loadError   error
}

func NewMockEventStore() *MockEventStore {
	return &MockEventStore{
		events: make(map[int64][]eventing.Event),
	}
}

func (m *MockEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []eventing.IStorableEvent, expectedVersion uint64) error {
	m.appendCalls++
	if m.appendError != nil {
		return m.appendError
	}

	// 转换并存储事件
	for _, evt := range events {
		e := eventing.Event{
			Message:       evt.(eventing.IEvent).(*eventing.Event).Message,
			AggregateID:   aggregateID,
			AggregateType: evt.GetAggregateType(),
			Version:       evt.GetVersion(),
			SchemaVersion: evt.GetSchemaVersion(),
		}
		m.events[aggregateID] = append(m.events[aggregateID], e)
	}
	return nil
}

func (m *MockEventStore) LoadEvents(ctx context.Context, aggregateID int64, afterVersion uint64) ([]eventing.Event, error) {
	m.loadCalls++
	if m.loadError != nil {
		return nil, m.loadError
	}

	events, exists := m.events[aggregateID]
	if !exists {
		return []eventing.Event{}, nil
	}

	// 过滤版本
	var filtered []eventing.Event
	for _, evt := range events {
		if evt.Version > afterVersion {
			filtered = append(filtered, evt)
		}
	}
	return filtered, nil
}

func (m *MockEventStore) StreamEvents(ctx context.Context, fromTime time.Time) ([]eventing.Event, error) {
	return nil, nil
}

func (m *MockEventStore) HasAggregate(ctx context.Context, aggregateID int64) (bool, error) {
	_, exists := m.events[aggregateID]
	return exists, nil
}

func (m *MockEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	events, exists := m.events[aggregateID]
	if !exists || len(events) == 0 {
		return 0, nil
	}
	return events[len(events)-1].Version, nil
}

// MockEventBus 模拟事件总线
type MockEventBus struct {
	publishedEvents []eventing.IEvent
	publishError    error
}

func (m *MockEventBus) PublishEvent(ctx context.Context, event eventing.IEvent) error {
	if m.publishError != nil {
		return m.publishError
	}
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

func (m *MockEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	if m.publishError != nil {
		return m.publishError
	}
	m.publishedEvents = append(m.publishedEvents, events...)
	return nil
}

func (m *MockEventBus) Subscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) error {
	return nil
}

func (m *MockEventBus) Unsubscribe(ctx context.Context, messageType string, handler messaging.IMessageHandler) error {
	return nil
}

func (m *MockEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	if evt, ok := message.(eventing.IEvent); ok {
		return m.PublishEvent(ctx, evt)
	}
	return nil
}

func (m *MockEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	events := make([]eventing.IEvent, 0, len(messages))
	for _, msg := range messages {
		if evt, ok := msg.(eventing.IEvent); ok {
			events = append(events, evt)
		}
	}
	return m.PublishEvents(ctx, events)
}

func (m *MockEventBus) Use(middleware messaging.IMiddleware) {}

func (m *MockEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) UnsubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) UnsubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

// 测试用例

func TestNewEventSourcedRepository(t *testing.T) {
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	tests := []struct {
		name    string
		opts    EventSourcedRepositoryOptions[*TestAggregate]
		wantErr bool
	}{
		{
			name: "有效配置",
			opts: EventSourcedRepositoryOptions[*TestAggregate]{
				AggregateType: "TestAggregate",
				Factory:       factory,
				EventStore:    eventStore,
			},
			wantErr: false,
		},
		{
			name: "缺少聚合类型",
			opts: EventSourcedRepositoryOptions[*TestAggregate]{
				Factory:    factory,
				EventStore: eventStore,
			},
			wantErr: true,
		},
		{
			name: "缺少工厂函数",
			opts: EventSourcedRepositoryOptions[*TestAggregate]{
				AggregateType: "TestAggregate",
				EventStore:    eventStore,
			},
			wantErr: true,
		},
		{
			name: "缺少事件存储",
			opts: EventSourcedRepositoryOptions[*TestAggregate]{
				AggregateType: "TestAggregate",
				Factory:       factory,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewEventSourcedRepository(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEventSourcedRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && repo == nil {
				t.Error("NewEventSourcedRepository() returned nil repo")
			}
		})
	}
}

func TestEventSourcedRepository_Save(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, err := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
	})
	if err != nil {
		t.Fatalf("创建仓储失败: %v", err)
	}

	t.Run("保存新聚合", func(t *testing.T) {
		agg := NewTestAggregate(1)
		evt := eventing.NewDomainEvent(1, "TestAggregate", "ValueSet", 1, 42)
		_ = agg.ApplyAndRecord(evt)

		if err := repo.Save(ctx, agg); err != nil {
			t.Errorf("Save() error = %v", err)
		}

		if eventStore.appendCalls != 1 {
			t.Errorf("期望调用 AppendEvents 1次, 实际 %d次", eventStore.appendCalls)
		}

		// 验证事件已标记为已提交
		if len(agg.GetUncommittedEvents()) != 0 {
			t.Error("事件未被标记为已提交")
		}
	})

	t.Run("保存无未提交事件的聚合", func(t *testing.T) {
		agg := NewTestAggregate(2)
		previousCalls := eventStore.appendCalls

		if err := repo.Save(ctx, agg); err != nil {
			t.Errorf("Save() error = %v", err)
		}

		if eventStore.appendCalls != previousCalls {
			t.Error("不应该调用 AppendEvents")
		}
	})

	t.Run("事件存储失败", func(t *testing.T) {
		eventStore.appendError = errors.New("存储失败")
		defer func() { eventStore.appendError = nil }()

		agg := NewTestAggregate(3)
		evt := eventing.NewDomainEvent(3, "TestAggregate", "ValueSet", 1, 99)
		_ = agg.ApplyAndRecord(evt)

		if err := repo.Save(ctx, agg); err == nil {
			t.Error("期望返回错误，但得到 nil")
		}
	})
}

func TestEventSourcedRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, _ := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
	})

	t.Run("加载已存在的聚合", func(t *testing.T) {
		// 先保存
		agg := NewTestAggregate(100)
		evt1 := eventing.NewDomainEvent(100, "TestAggregate", "ValueSet", 1, 10)
		evt2 := eventing.NewDomainEvent(100, "TestAggregate", "ValueIncremented", 2, 5)
		_ = agg.ApplyAndRecord(evt1)
		_ = agg.ApplyAndRecord(evt2)
		_ = repo.Save(ctx, agg)

		// 重新加载
		loaded, err := repo.GetByID(ctx, 100)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}

		if loaded.Value != 15 {
			t.Errorf("期望 Value = 15, 实际 = %d", loaded.Value)
		}

		if loaded.GetVersion() != 2 {
			t.Errorf("期望 Version = 2, 实际 = %d", loaded.GetVersion())
		}
	})

	t.Run("加载不存在的聚合", func(t *testing.T) {
		loaded, err := repo.GetByID(ctx, 999)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}

		if loaded.GetVersion() != 0 {
			t.Error("新聚合版本应为 0")
		}

		if loaded.Value != 0 {
			t.Error("新聚合状态应为初始值")
		}
	})

	t.Run("加载事件失败", func(t *testing.T) {
		eventStore.loadError = errors.New("加载失败")
		defer func() { eventStore.loadError = nil }()

		_, err := repo.GetByID(ctx, 100)
		if err == nil {
			t.Error("期望返回错误，但得到 nil")
		}
	})
}

func TestEventSourcedRepository_Exists(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, _ := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
	})

	// 保存一个聚合
	agg := NewTestAggregate(200)
	evt := eventing.NewDomainEvent(200, "TestAggregate", "ValueSet", 1, 1)
	_ = agg.ApplyAndRecord(evt)
	_ = repo.Save(ctx, agg)

	t.Run("存在的聚合", func(t *testing.T) {
		exists, err := repo.Exists(ctx, 200)
		if err != nil {
			t.Fatalf("Exists() error = %v", err)
		}
		if !exists {
			t.Error("期望返回 true")
		}
	})

	t.Run("不存在的聚合", func(t *testing.T) {
		exists, err := repo.Exists(ctx, 999)
		if err != nil {
			t.Fatalf("Exists() error = %v", err)
		}
		if exists {
			t.Error("期望返回 false")
		}
	})
}

func TestEventSourcedRepository_GetAggregateVersion(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, _ := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
	})

	// 保存多个版本
	agg := NewTestAggregate(300)
	evt1 := eventing.NewDomainEvent(300, "TestAggregate", "ValueSet", 1, 1)
	evt2 := eventing.NewDomainEvent(300, "TestAggregate", "ValueIncremented", 2, 1)
	evt3 := eventing.NewDomainEvent(300, "TestAggregate", "ValueIncremented", 3, 1)
	_ = agg.ApplyAndRecord(evt1)
	_ = agg.ApplyAndRecord(evt2)
	_ = agg.ApplyAndRecord(evt3)
	_ = repo.Save(ctx, agg)

	version, err := repo.GetAggregateVersion(ctx, 300)
	if err != nil {
		t.Fatalf("GetAggregateVersion() error = %v", err)
	}

	if version != 3 {
		t.Errorf("期望版本 = 3, 实际 = %d", version)
	}
}

func TestEventSourcedRepository_WithEventBus(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	eventBus := &MockEventBus{}
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, _ := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
		EventBus:      eventBus,
		PublishEvents: true,
	})

	agg := NewTestAggregate(400)
	evt := eventing.NewDomainEvent(400, "TestAggregate", "ValueSet", 1, 42)
	_ = agg.ApplyAndRecord(evt)

	if err := repo.Save(ctx, agg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if len(eventBus.publishedEvents) != 1 {
		t.Errorf("期望发布 1 个事件, 实际 %d", len(eventBus.publishedEvents))
	}
}

func TestEventSourcedRepository_WithSnapshot(t *testing.T) {
	t.Skip("Snapshot manager requires proper initialization - skipping for now")

	// 注意：此测试需要完整的快照存储实现才能正常运行
	// 当前仅验证不带快照管理器的基本功能
}

func TestEventSourcedRepository_GetEventHistory(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, _ := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
	})

	// 保存多个事件
	agg := NewTestAggregate(600)
	evt1 := eventing.NewDomainEvent(600, "TestAggregate", "ValueSet", 1, 10)
	evt2 := eventing.NewDomainEvent(600, "TestAggregate", "ValueIncremented", 2, 5)
	_ = agg.ApplyAndRecord(evt1)
	_ = agg.ApplyAndRecord(evt2)
	_ = repo.Save(ctx, agg)

	history, err := repo.GetEventHistory(ctx, 600)
	if err != nil {
		t.Fatalf("GetEventHistory() error = %v", err)
	}

	if len(history) != 2 {
		t.Errorf("期望 2 个事件, 实际 %d", len(history))
	}
}

func TestEventSourcedRepository_GetEventHistoryAfter(t *testing.T) {
	ctx := context.Background()
	eventStore := NewMockEventStore()
	factory := func(id int64) *TestAggregate {
		return NewTestAggregate(id)
	}

	repo, _ := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       factory,
		EventStore:    eventStore,
	})

	// 保存3个事件
	agg := NewTestAggregate(700)
	evt1 := eventing.NewDomainEvent(700, "TestAggregate", "ValueSet", 1, 10)
	evt2 := eventing.NewDomainEvent(700, "TestAggregate", "ValueIncremented", 2, 5)
	evt3 := eventing.NewDomainEvent(700, "TestAggregate", "ValueIncremented", 3, 5)
	_ = agg.ApplyAndRecord(evt1)
	_ = agg.ApplyAndRecord(evt2)
	_ = agg.ApplyAndRecord(evt3)
	_ = repo.Save(ctx, agg)

	// 获取版本1之后的事件
	history, err := repo.GetEventHistoryAfter(ctx, 700, 1)
	if err != nil {
		t.Fatalf("GetEventHistoryAfter() error = %v", err)
	}

	if len(history) != 2 {
		t.Errorf("期望 2 个事件, 实际 %d", len(history))
	}
}
