package eventsourced

import (
	"testing"

	"gochen/errors"
)

// 测试用的简单事件。
type TestEvent struct {
	eventType string
	data      string
}

// EventType 返回事件类型标识。
func (e *TestEvent) EventType() string {
	return e.eventType
}

// 测试用的聚合。
type TestAggregate struct {
	*EventSourcedAggregate[int64]
	Data string
}

// NewTestAggregate 创建并返回 TestAggregate 实例。
func NewTestAggregate(id int64) *TestAggregate {
	registry := NewMetadataRegistry()
	a := &TestAggregate{}
	agg, err := InitAggregate[int64](registry, a, id, "TestAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyTestEvent 应用 TestEvent。
func (a *TestAggregate) ApplyTestEvent(evt *TestEvent) {
	a.Data = evt.data
}

// TestNewEventSourcedAggregate 验证 NewEventSourcedAggregate。
func TestNewEventSourcedAggregate(t *testing.T) {
	t.Run("创建聚合根", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](100, "TestAggregate")

		if agg.GetID() != 100 {
			t.Errorf("expected ID 100, got %d", agg.GetID())
		}
		if agg.GetVersion() != 0 {
			t.Errorf("expected version 0, got %d", agg.GetVersion())
		}
		if agg.GetExpectedVersion() != 0 {
			t.Errorf("expected expectedVersion 0, got %d", agg.GetExpectedVersion())
		}
		if agg.GetAggregateType() != "TestAggregate" {
			t.Errorf("expected type TestAggregate, got %s", agg.GetAggregateType())
		}
		if len(agg.GetUncommittedEvents()) != 0 {
			t.Errorf("expected 0 uncommitted events, got %d", len(agg.GetUncommittedEvents()))
		}
	})

	t.Run("创建不同ID类型的聚合根", func(t *testing.T) {
		aggInt := NewEventSourcedAggregate[int64](42, "IntAggregate")
		if aggInt.GetID() != 42 {
			t.Errorf("int64: expected ID 42, got %d", aggInt.GetID())
		}

		aggStr := NewEventSourcedAggregate[string]("test-id", "StringAggregate")
		if aggStr.GetID() != "test-id" {
			t.Errorf("string: expected ID 'test-id', got %s", aggStr.GetID())
		}
	})
}

// TestApplyEvent 验证 ApplyEvent。
func TestApplyEvent(t *testing.T) {
	t.Run("nil event returns invalid input and does not increment version", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")

		err := agg.ApplyEvent(nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("expected InvalidInput, got: %v", err)
		}
		if agg.GetVersion() != 0 {
			t.Fatalf("expected version unchanged, got %d", agg.GetVersion())
		}
	})

	t.Run("未绑定 metadata 返回错误", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		err := agg.ApplyEvent(&TestEvent{eventType: "TestEvent", data: "hello"})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, errors.Internal) {
			t.Fatalf("expected Internal, got %v", err)
		}
	})

	t.Run("自动路由 handler 更新状态和版本", func(t *testing.T) {
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
		if agg.GetExpectedVersion() != 0 {
			t.Errorf("expected expectedVersion unchanged at 0, got %d", agg.GetExpectedVersion())
		}
	})

	t.Run("多次 ApplyEvent 版本正确累加", func(t *testing.T) {
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

// TestRecordUncommittedEvent 验证 RecordUncommittedEvent。
func TestRecordUncommittedEvent(t *testing.T) {
	t.Run("nil事件返回 InvalidInput", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		err := agg.RecordUncommittedEvent(nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("expected InvalidInput, got: %v", err)
		}
	})

	t.Run("返回事件副本而非引用", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		evt := &TestEvent{eventType: "TestEvent", data: "test"}
		if err := agg.RecordUncommittedEvent(evt); err != nil {
			t.Fatalf("RecordUncommittedEvent failed: %v", err)
		}

		events1 := agg.GetUncommittedEvents()
		events1[0] = &TestEvent{eventType: "Modified", data: "modified"}

		events2 := agg.GetUncommittedEvents()
		if events2[0].EventType() != "TestEvent" {
			t.Error("modifying returned slice should not affect internal state")
		}
	})
}

// TestApplyAndRecord 验证 ApplyAndRecord。
func TestApplyAndRecord(t *testing.T) {
	t.Run("nil事件返回InvalidInput且不产生副作用", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")

		err := agg.ApplyAndRecord(nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("expected InvalidInput, got: %v", err)
		}
		if agg.GetVersion() != 0 {
			t.Fatalf("expected version unchanged, got %d", agg.GetVersion())
		}
		if len(agg.GetUncommittedEvents()) != 0 {
			t.Fatalf("expected 0 uncommitted events, got %d", len(agg.GetUncommittedEvents()))
		}
	})

	t.Run("自动路由 handler 正确更新状态、版本与基线版本", func(t *testing.T) {
		agg := NewTestAggregate(1)

		requireEvent := func(data string, expectedVersion uint64, expectedBase uint64, expectedCount int) {
			t.Helper()
			err := agg.ApplyAndRecord(&TestEvent{eventType: "TestEvent", data: data})
			if err != nil {
				t.Fatalf("ApplyAndRecord failed: %v", err)
			}
			if agg.Data != data {
				t.Fatalf("expected Data %q, got %q", data, agg.Data)
			}
			if agg.GetVersion() != expectedVersion {
				t.Fatalf("expected version %d, got %d", expectedVersion, agg.GetVersion())
			}
			if agg.GetExpectedVersion() != expectedBase {
				t.Fatalf("expected expectedVersion %d, got %d", expectedBase, agg.GetExpectedVersion())
			}
			if len(agg.GetUncommittedEvents()) != expectedCount {
				t.Fatalf("expected %d uncommitted events, got %d", expectedCount, len(agg.GetUncommittedEvents()))
			}
		}

		requireEvent("hello", 1, 0, 1)
		requireEvent("world", 2, 0, 2)
	})

	t.Run("快照恢复后新增事件保留显式 expected version", func(t *testing.T) {
		agg := NewTestAggregate(1)
		agg.SetVersion(5)

		err := agg.ApplyAndRecord(&TestEvent{eventType: "TestEvent", data: "after-snapshot"})
		if err != nil {
			t.Fatalf("ApplyAndRecord failed: %v", err)
		}
		if agg.GetVersion() != 6 {
			t.Fatalf("expected version 6, got %d", agg.GetVersion())
		}
		if agg.GetExpectedVersion() != 5 {
			t.Fatalf("expected expectedVersion 5, got %d", agg.GetExpectedVersion())
		}
	})
}

// TestMarkEventsAsCommitted 验证 MarkEventsAsCommitted。
func TestMarkEventsAsCommitted(t *testing.T) {
	t.Run("清空未提交事件并推进 expected version", func(t *testing.T) {
		agg := NewTestAggregate(1)
		if err := agg.ApplyAndRecord(&TestEvent{eventType: "TestEvent", data: "test"}); err != nil {
			t.Fatalf("ApplyAndRecord failed: %v", err)
		}

		agg.MarkEventsAsCommitted()

		if len(agg.GetUncommittedEvents()) != 0 {
			t.Fatalf("expected 0 uncommitted events, got %d", len(agg.GetUncommittedEvents()))
		}
		if agg.GetExpectedVersion() != agg.GetVersion() {
			t.Fatalf("expected expectedVersion=%d, got %d", agg.GetVersion(), agg.GetExpectedVersion())
		}
	})
}

// TestBindMetadata 验证 BindMetadata。
func TestBindMetadata(t *testing.T) {
	registry := newTestMetadataRegistry(t)
	t.Run("显式绑定预编译 metadata 后可自动分发事件", func(t *testing.T) {
		agg := &autoApplyAggregate{
			EventSourcedAggregate: NewEventSourcedAggregate[int64](1, "AutoApply"),
		}
		meta, err := registry.Resolve(agg, "AutoApply")
		if err != nil {
			t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
		}
		if err := agg.BindMetadata(agg, meta); err != nil {
			t.Fatalf("BindMetadata failed: %v", err)
		}

		err = agg.ApplyAndRecord(&userCreatedV1{Name: "alice"})
		if err != nil {
			t.Fatalf("ApplyAndRecord failed: %v", err)
		}
		if agg.V1Name != "alice" {
			t.Fatalf("expected V1Name=alice, got %s", agg.V1Name)
		}
	})

	t.Run("self 类型与 metadata 不匹配返回错误", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "AutoApply")
		meta, err := registry.Resolve(&autoApplyAggregate{}, "AutoApply")
		if err != nil {
			t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
		}
		err = agg.BindMetadata(agg, meta)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, errors.Conflict) {
			t.Fatalf("expected Conflict, got %v", err)
		}
	})
}

func TestMetadataRegistryRegisterAndInitAggregate(t *testing.T) {
	t.Run("注册 metadata 后 Resolve 直接复用缓存", func(t *testing.T) {
		registry := NewMetadataRegistry()
		first, err := registry.Register(&TestAggregate{}, "TestAggregate")
		if err != nil {
			t.Fatalf("MetadataRegistry.Register failed: %v", err)
		}
		second, err := registry.Resolve(&TestAggregate{}, "TestAggregate")
		if err != nil {
			t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
		}
		if first != second {
			t.Fatalf("expected MetadataRegistry.Resolve to reuse registered metadata")
		}
	})

	t.Run("批量注册后重复解析仍复用同一份 metadata", func(t *testing.T) {
		registry := NewMetadataRegistry()
		if err := registry.RegisterSet(
			MetadataRegistration{Sample: &TestAggregate{}, AggregateType: "TestAggregate"},
			MetadataRegistration{Sample: nil, AggregateType: "ignored"},
		); err != nil {
			t.Fatalf("MetadataRegistry.RegisterSet failed: %v", err)
		}

		first, err := registry.Resolve(&TestAggregate{}, "TestAggregate")
		if err != nil {
			t.Fatalf("MetadataRegistry.Resolve failed: %v", err)
		}
		second, err := registry.Register(&TestAggregate{}, "TestAggregate")
		if err != nil {
			t.Fatalf("MetadataRegistry.Register failed: %v", err)
		}
		if first != second {
			t.Fatalf("expected batch registration to keep using cached metadata")
		}
	})

	t.Run("InitAggregate 隐藏 metadata 解析与 bind 细节", func(t *testing.T) {
		registry := NewMetadataRegistry()
		agg := &TestAggregate{}
		esa, err := InitAggregate[int64](registry, agg, 7, "TestAggregate")
		if err != nil {
			t.Fatalf("InitAggregate failed: %v", err)
		}
		agg.EventSourcedAggregate = esa

		if err := agg.ApplyAndRecord(&TestEvent{eventType: "TestEvent", data: "registered"}); err != nil {
			t.Fatalf("ApplyAndRecord failed: %v", err)
		}
		if agg.Data != "registered" {
			t.Fatalf("expected Data=registered, got %q", agg.Data)
		}
	})
}

// TestSetVersion 验证 SetVersion。
func TestSetVersion(t *testing.T) {
	t.Run("SetVersion 同步设置 version 与 expectedVersion", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](1, "Test")
		agg.SetVersion(42)

		if agg.GetVersion() != 42 {
			t.Errorf("expected version 42, got %d", agg.GetVersion())
		}
		if agg.GetExpectedVersion() != 42 {
			t.Errorf("expected expectedVersion 42, got %d", agg.GetExpectedVersion())
		}
	})
}

// TestApplyEventErrorContext 验证 ApplyEventErrorContext。
func TestApplyEventErrorContext(t *testing.T) {
	t.Run("nil事件错误包含 aggregate_type 和 aggregate_id", func(t *testing.T) {
		agg := NewEventSourcedAggregate[int64](42, "MyAggregate")

		err := agg.ApplyEvent(nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}

		var appErr *errors.AppError
		if !errors.As(err, &appErr) || appErr == nil {
			t.Fatalf("expected *errors.AppError, got %T", err)
		}

		details := appErr.Details()
		if details["aggregate_type"] != "MyAggregate" {
			t.Errorf("expected aggregate_type 'MyAggregate', got %v", details["aggregate_type"])
		}
		if details["aggregate_id"] != int64(42) {
			t.Errorf("expected aggregate_id 42, got %v", details["aggregate_id"])
		}
	})
}

// BenchmarkApplyEvent 用于评估 ApplyEvent 的性能。
func BenchmarkApplyEvent(b *testing.B) {
	agg := NewTestAggregate(1)
	evt := &TestEvent{eventType: "TestEvent", data: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agg.ApplyEvent(evt)
	}
}

// BenchmarkApplyAndRecord 用于评估 ApplyAndRecord 的性能。
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

// BenchmarkApplyEventWithAutoRoute 用于评估 ApplyEventWithAutoRoute 的性能。
func BenchmarkApplyEventWithAutoRoute(b *testing.B) {
	agg := newAutoApplyAggregate(1)
	evt := &userCreatedV1{Name: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = agg.ApplyEvent(evt)
	}
}
