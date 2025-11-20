package entity_test

import (
	"sync"
	"testing"
	"time"

	"gochen/domain/entity"
	"gochen/eventing"
	msg "gochen/messaging"
)

// TestEventSourcedAggregate_ConcurrentAccess 测试并发访问安全性
func TestEventSourcedAggregate_ConcurrentAccess(t *testing.T) {
	agg := entity.NewEventSourcedAggregate[int64](1, "TestAggregate")

	// 创建测试事件
	evt := &eventing.Event{
		Message: msg.Message{
			ID:        "evt-1",
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  make(map[string]any),
		},
		AggregateID:   1,
		AggregateType: "TestAggregate",
		Version:       1,
	}

	// 并发读写测试
	var wg sync.WaitGroup
	concurrency := 100

	// 并发写入
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agg.AddDomainEvent(evt)
		}()
	}

	// 并发读取
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = agg.GetID()
			_ = agg.GetVersion()
			_ = agg.GetDomainEvents()
		}()
	}

	wg.Wait()

	// 验证状态一致性
	events := agg.GetUncommittedEvents()
	if len(events) != concurrency {
		t.Errorf("Expected %d events, got %d", concurrency, len(events))
	}
}

// TestEventSourcedAggregate_ApplyAndRecord 测试事件应用和记录
func TestEventSourcedAggregate_ApplyAndRecord(t *testing.T) {
	agg := entity.NewEventSourcedAggregate[int64](1, "TestAggregate")

	evt := &eventing.Event{
		Message: msg.Message{
			ID:        "evt-1",
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  make(map[string]any),
		},
		AggregateID:   1,
		AggregateType: "TestAggregate",
		Version:       1,
	}

	// 应用并记录事件
	err := agg.ApplyAndRecord(evt)
	if err != nil {
		t.Fatalf("ApplyAndRecord failed: %v", err)
	}

	// 验证版本号递增
	if agg.GetVersion() != 1 {
		t.Errorf("Expected version 1, got %d", agg.GetVersion())
	}

	// 验证事件已记录
	events := agg.GetUncommittedEvents()
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	// 标记为已提交
	agg.MarkEventsAsCommitted()

	// 验证事件已清空
	events = agg.GetUncommittedEvents()
	if len(events) != 0 {
		t.Errorf("Expected 0 events after commit, got %d", len(events))
	}
}

// TestEventSourcedAggregate_LoadFromHistory 测试从历史事件重建
func TestEventSourcedAggregate_LoadFromHistory(t *testing.T) {
	agg := entity.NewEventSourcedAggregate[int64](1, "TestAggregate")

	// 创建历史事件
	events := []eventing.IEvent{
		&eventing.Event{
			Message: msg.Message{ID: "evt-1", Type: "Event1", Timestamp: time.Now(), Metadata: make(map[string]any)},
			Version: 1,
		},
		&eventing.Event{
			Message: msg.Message{ID: "evt-2", Type: "Event2", Timestamp: time.Now(), Metadata: make(map[string]any)},
			Version: 2,
		},
		&eventing.Event{
			Message: msg.Message{ID: "evt-3", Type: "Event3", Timestamp: time.Now(), Metadata: make(map[string]any)},
			Version: 3,
		},
	}

	// 从历史重建
	err := agg.LoadFromHistory(events)
	if err != nil {
		t.Fatalf("LoadFromHistory failed: %v", err)
	}

	// 验证版本号
	if agg.GetVersion() != 3 {
		t.Errorf("Expected version 3, got %d", agg.GetVersion())
	}

	// 验证没有未提交事件（历史事件不应记录为未提交）
	uncommitted := agg.GetUncommittedEvents()
	if len(uncommitted) != 0 {
		t.Errorf("Expected 0 uncommitted events, got %d", len(uncommitted))
	}
}
