package eventsourced

import (
	"testing"

	"gochen/domain"
)

// 测试用的简单事件
type TestEvent struct {
	eventType string
	data      string
}

func (e *TestEvent) EventType() string {
	return e.eventType
}

// 测试用的聚合
type TestAggregate struct {
	*EventSourcedAggregate[int64]
	Data string
}

func NewTestAggregate(id int64) *TestAggregate {
	return &TestAggregate{
		EventSourcedAggregate: NewEventSourcedAggregate[int64](id, "TestAggregate"),
	}
}

func (a *TestAggregate) ApplyEvent(evt domain.IDomainEvent) error {
	// 先更新业务状态
	switch e := evt.(type) {
	case *TestEvent:
		a.Data = e.data
	}
	// 再调用基类递增版本
	return a.EventSourcedAggregate.ApplyEvent(evt)
}

// TestNewEventSourcedAggregate 测试聚合根创建
func TestNewEventSourcedAggregate(t *testing.T) {
	t.Run("创建聚合根", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](100, "TestAggregate")

		if agg.GetID() != 100 {
			t.Errorf("expected ID 100, got %d", agg.GetID())
		}

		if agg.GetVersion() != 0 {
			t.Errorf("expected version 0, got %d", agg.GetVersion())
		}

		if agg.GetAggregateType() != "TestAggregate" {
			t.Errorf("expected type TestAggregate, got %s", agg.GetAggregateType())
		}

		if len(agg.GetUncommittedEvents()) != 0 {
			t.Errorf("expected 0 uncommitted events, got %d", len(agg.GetUncommittedEvents()))
		}
	})

	t.Run("创建不同ID类型的聚合根", func(t *testing.T) {
		// int64
		aggInt := NewEventSourcedAggregate[int64](42, "IntAggregate")
		if aggInt.GetID() != 42 {
			t.Errorf("int64: expected ID 42, got %d", aggInt.GetID())
		}

		// string
		aggStr := NewEventSourcedAggregate[string]("test-id", "StringAggregate")
		if aggStr.GetID() != "test-id" {
			t.Errorf("string: expected ID 'test-id', got %s", aggStr.GetID())
		}
	})
}

// TestApplyEvent 测试事件应用
func TestApplyEvent(t *testing.T) {
	t.Run("基类ApplyEvent递增版本", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "test"}

		err := agg.ApplyEvent(evt)
		if err != nil {
			t.Fatalf("ApplyEvent failed: %v", err)
		}

		if agg.GetVersion() != 1 {
			t.Errorf("expected version 1 after first event, got %d", agg.GetVersion())
		}

		// 应用第二个事件
		err = agg.ApplyEvent(evt)
		if err != nil {
			t.Fatalf("ApplyEvent failed: %v", err)
		}

		if agg.GetVersion() != 2 {
			t.Errorf("expected version 2 after second event, got %d", agg.GetVersion())
		}
	})

	t.Run("子类ApplyEvent正确更新状态和版本", func(t *testing.T) {
		agg := NewTestAggregate(1)
		evt := &TestEvent{eventType: "TestEvent", data: "hello"}

		err := agg.ApplyEvent(evt)
		if err != nil {
			t.Fatalf("ApplyEvent failed: %v", err)
		}

		if agg.Data != "hello" {
			t.Errorf("expected Data 'hello', got '%s'", agg.Data)
		}

		if agg.GetVersion() != 1 {
			t.Errorf("expected version 1, got %d", agg.GetVersion())
		}
	})

	t.Run("多次ApplyEvent版本正确累加", func(t *testing.T) {
		agg := NewTestAggregate(1)

		for i := 1; i <= 5; i++ {
			evt := &TestEvent{eventType: "TestEvent", data: string(rune('a' + i))}
			err := agg.ApplyEvent(evt)
			if err != nil {
				t.Fatalf("ApplyEvent #%d failed: %v", i, err)
			}

			if agg.GetVersion() != uint64(i) {
				t.Errorf("after event #%d, expected version %d, got %d", i, i, agg.GetVersion())
			}
		}
	})
}

// TestAddDomainEvent 测试添加领域事件
func TestAddDomainEvent(t *testing.T) {
	t.Run("添加事件到未提交列表", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "test"}

		agg.AddDomainEvent(evt)

		events := agg.GetUncommittedEvents()
		if len(events) != 1 {
			t.Fatalf("expected 1 uncommitted event, got %d", len(events))
		}

		if events[0].EventType() != "TestEvent" {
			t.Errorf("expected event type 'TestEvent', got '%s'", events[0].EventType())
		}
	})

	t.Run("多次添加事件", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")

		for i := 0; i < 3; i++ {
			evt := &TestEvent{eventType: "TestEvent", data: string(rune('a' + i))}
			agg.AddDomainEvent(evt)
		}

		events := agg.GetUncommittedEvents()
		if len(events) != 3 {
			t.Fatalf("expected 3 uncommitted events, got %d", len(events))
		}
	})

	t.Run("初始化nil切片后添加事件", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		agg.uncommittedEvents = nil

		evt := &TestEvent{eventType: "TestEvent", data: "test"}
		agg.AddDomainEvent(evt)

		if agg.uncommittedEvents == nil {
			t.Fatal("uncommittedEvents should not be nil after AddDomainEvent")
		}

		if len(agg.uncommittedEvents) != 1 {
			t.Errorf("expected 1 event, got %d", len(agg.uncommittedEvents))
		}
	})
}

// TestGetUncommittedEvents 测试获取未提交事件
func TestGetUncommittedEvents(t *testing.T) {
	t.Run("返回事件副本而非引用", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "test"}
		agg.AddDomainEvent(evt)

		events1 := agg.GetUncommittedEvents()

		// 修改返回的切片不应影响内部状态
		if len(events1) > 0 {
			events1[0] = &TestEvent{eventType: "Modified", data: "modified"}
		}

		events2 := agg.GetUncommittedEvents()
		if events2[0].EventType() != "TestEvent" {
			t.Error("modifying returned slice should not affect internal state")
		}
	})

	t.Run("空切片返回空", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")

		events := agg.GetUncommittedEvents()
		if len(events) != 0 {
			t.Errorf("expected 0 events, got %d", len(events))
		}
	})
}

// TestMarkEventsAsCommitted 测试标记事件为已提交
func TestMarkEventsAsCommitted(t *testing.T) {
	t.Run("清空未提交事件", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "test"}
		agg.AddDomainEvent(evt)

		if len(agg.GetUncommittedEvents()) != 1 {
			t.Fatal("setup failed: expected 1 uncommitted event")
		}

		agg.MarkEventsAsCommitted()

		if len(agg.GetUncommittedEvents()) != 0 {
			t.Errorf("expected 0 uncommitted events after MarkEventsAsCommitted, got %d", len(agg.GetUncommittedEvents()))
		}
	})

	t.Run("版本号不受影响", func(t *testing.T) {
		agg := NewTestAggregate(1)
		evt := &TestEvent{eventType: "TestEvent", data: "test"}

		agg.ApplyEvent(evt)
		agg.AddDomainEvent(evt)

		versionBefore := agg.GetVersion()
		agg.MarkEventsAsCommitted()
		versionAfter := agg.GetVersion()

		if versionBefore != versionAfter {
			t.Errorf("version should not change after MarkEventsAsCommitted: before=%d, after=%d", versionBefore, versionAfter)
		}
	})
}

// TestClearDomainEvents 测试清空领域事件
func TestClearDomainEvents(t *testing.T) {
	t.Run("清空领域事件", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "test"}
		agg.AddDomainEvent(evt)

		agg.ClearDomainEvents()

		events := agg.GetUncommittedEvents()
		if events != nil && len(events) != 0 {
			t.Errorf("expected nil or empty slice after ClearDomainEvents, got %d events", len(events))
		}
	})
}

// TestApplyAndRecord 测试应用并记录事件
func TestApplyAndRecord(t *testing.T) {
	t.Run("基类ApplyAndRecord正确工作", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "hello"}

		err := agg.ApplyAndRecord(evt)
		if err != nil {
			t.Fatalf("ApplyAndRecord failed: %v", err)
		}

		// 检查版本递增
		if agg.GetVersion() != 1 {
			t.Errorf("expected version 1, got %d", agg.GetVersion())
		}

		// 检查事件记录
		events := agg.GetUncommittedEvents()
		if len(events) != 1 {
			t.Fatalf("expected 1 uncommitted event, got %d", len(events))
		}

		if events[0].EventType() != "TestEvent" {
			t.Errorf("expected event type 'TestEvent', got '%s'", events[0].EventType())
		}
	})

	t.Run("子类需要显式调用ApplyEvent和AddDomainEvent", func(t *testing.T) {
		// 注意：由于 Go 没有虚函数机制，基类的 ApplyAndRecord 调用的是基类的 ApplyEvent
		// 子类如果需要自定义事件应用逻辑，应该自己实现 ApplyAndRecord 或者
		// 显式调用 agg.ApplyEvent(evt) + agg.AddDomainEvent(evt)
		agg := NewTestAggregate(1)
		evt := &TestEvent{eventType: "TestEvent", data: "hello"}

		// 方式1：显式调用
		err := agg.ApplyEvent(evt)
		if err != nil {
			t.Fatalf("ApplyEvent failed: %v", err)
		}
		agg.AddDomainEvent(evt)

		// 检查状态更新
		if agg.Data != "hello" {
			t.Errorf("expected Data 'hello', got '%s'", agg.Data)
		}

		// 检查版本递增
		if agg.GetVersion() != 1 {
			t.Errorf("expected version 1, got %d", agg.GetVersion())
		}

		// 检查事件记录
		events := agg.GetUncommittedEvents()
		if len(events) != 1 {
			t.Fatalf("expected 1 uncommitted event, got %d", len(events))
		}
	})

	t.Run("多次应用事件", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")

		for i := 1; i <= 3; i++ {
			evt := &TestEvent{eventType: "TestEvent", data: string(rune('a' + i))}
			err := agg.ApplyAndRecord(evt)
			if err != nil {
				t.Fatalf("ApplyAndRecord #%d failed: %v", i, err)
			}
		}

		if agg.GetVersion() != 3 {
			t.Errorf("expected version 3, got %d", agg.GetVersion())
		}

		events := agg.GetUncommittedEvents()
		if len(events) != 3 {
			t.Errorf("expected 3 uncommitted events, got %d", len(events))
		}
	})
}

// TestValidate 测试验证方法
func TestValidate(t *testing.T) {
	t.Run("默认验证返回nil", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")

		err := agg.Validate()
		if err != nil {
			t.Errorf("default Validate should return nil, got %v", err)
		}
	})
}

// TestGetDomainEvents 测试GetDomainEvents与GetUncommittedEvents等价
func TestGetDomainEvents(t *testing.T) {
	t.Run("GetDomainEvents与GetUncommittedEvents返回相同内容", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt1 := &TestEvent{eventType: "Event1", data: "data1"}
		evt2 := &TestEvent{eventType: "Event2", data: "data2"}

		agg.AddDomainEvent(evt1)
		agg.AddDomainEvent(evt2)

		domainEvents := agg.GetDomainEvents()
		uncommittedEvents := agg.GetUncommittedEvents()

		if len(domainEvents) != len(uncommittedEvents) {
			t.Errorf("length mismatch: GetDomainEvents=%d, GetUncommittedEvents=%d", len(domainEvents), len(uncommittedEvents))
		}

		for i := range domainEvents {
			if domainEvents[i].EventType() != uncommittedEvents[i].EventType() {
				t.Errorf("event %d type mismatch: %s vs %s", i, domainEvents[i].EventType(), uncommittedEvents[i].EventType())
			}
		}
	})
}

// BenchmarkApplyEvent 基准测试：ApplyEvent
func BenchmarkApplyEvent(b *testing.B) {
	agg := NewTestAggregate(1)
	evt := &TestEvent{eventType: "TestEvent", data: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agg.ApplyEvent(evt)
	}
}

// BenchmarkApplyAndRecord 基准测试：ApplyAndRecord
func BenchmarkApplyAndRecord(b *testing.B) {
	agg := NewTestAggregate(1)
	evt := &TestEvent{eventType: "TestEvent", data: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agg.ApplyAndRecord(evt)
		if i%100 == 0 {
			agg.MarkEventsAsCommitted()
		}
	}
}
