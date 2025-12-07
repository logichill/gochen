package eventsourced

import (
	"context"
	"errors"
	"testing"

	"gochen/domain"
)

// Mock 事件存储（实现 IEventStore[int64]）
type mockEventStore struct {
	appendCalled          bool
	restoreCalled         bool
	existsCalled          bool
	getVersionCalled      bool
	appendEvents          []domain.IDomainEvent
	appendExpectedVersion uint64
	restoreEvents         []domain.IDomainEvent
	restoreVersion        uint64
	exists                bool
	version               uint64
	appendError           error
	restoreError          error
	existsError           error
	versionError          error
}

func (m *mockEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []domain.IDomainEvent, expectedVersion uint64) error {
	m.appendCalled = true
	m.appendEvents = events
	m.appendExpectedVersion = expectedVersion
	return m.appendError
}

func (m *mockEventStore) RestoreAggregate(ctx context.Context, aggregate IEventSourcedAggregate[int64]) (uint64, error) {
	m.restoreCalled = true
	if m.restoreError != nil {
		return 0, m.restoreError
	}

	// 模拟事件重放
	for _, evt := range m.restoreEvents {
		if err := aggregate.ApplyEvent(evt); err != nil {
			return 0, err
		}
	}

	return m.restoreVersion, nil
}

func (m *mockEventStore) Exists(ctx context.Context, aggregateID int64) (bool, error) {
	m.existsCalled = true
	return m.exists, m.existsError
}

func (m *mockEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	m.getVersionCalled = true
	return m.version, m.versionError
}

// 确保 mockEventStore 实现 IEventStore[int64] 接口
var _ IEventStore[int64] = (*mockEventStore)(nil)

// TestNewEventSourcedRepository 测试仓储创建
func TestNewEventSourcedRepository(t *testing.T) {
	store := &mockEventStore{}

	t.Run("成功创建仓储", func(t *testing.T) {
		repo, err := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		if err != nil {
			t.Fatalf("NewEventSourcedRepository failed: %v", err)
		}

		if repo == nil {
			t.Fatal("repo should not be nil")
		}

		if repo.aggregateType != "TestAggregate" {
			t.Errorf("expected aggregateType 'TestAggregate', got '%s'", repo.aggregateType)
		}
	})

	t.Run("aggregateType为空返回错误", func(t *testing.T) {
		_, err := NewEventSourcedRepository[*TestAggregate](
			"",
			NewTestAggregate,
			store,
		)

		if err == nil {
			t.Fatal("expected error for empty aggregateType, got nil")
		}

		if err.Error() != "aggregate type cannot be empty" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("factory为nil返回错误", func(t *testing.T) {
		_, err := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			nil,
			store,
		)

		if err == nil {
			t.Fatal("expected error for nil factory, got nil")
		}

		if err.Error() != "aggregate factory cannot be nil" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("store为nil返回错误", func(t *testing.T) {
		_, err := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			nil,
		)

		if err == nil {
			t.Fatal("expected error for nil store, got nil")
		}

		if err.Error() != "event store cannot be nil" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// TestRepositorySave 测试保存聚合
func TestRepositorySave(t *testing.T) {
	t.Run("保存有未提交事件的聚合", func(t *testing.T) {
		store := &mockEventStore{}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		agg := NewTestAggregate(1)
		evt1 := &TestEvent{eventType: "Event1", data: "data1"}
		evt2 := &TestEvent{eventType: "Event2", data: "data2"}

		// 应用并记录事件
		agg.ApplyAndRecord(evt1)
		agg.ApplyAndRecord(evt2)

		// 保存
		err := repo.Save(context.Background(), agg)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// 验证调用
		if !store.appendCalled {
			t.Error("AppendEvents should have been called")
		}

		// 验证 expectedVersion 计算
		// currentVersion=2, eventCount=2, expectedVersion=0
		if store.appendExpectedVersion != 0 {
			t.Errorf("expected expectedVersion 0, got %d", store.appendExpectedVersion)
		}

		// 验证事件数量
		if len(store.appendEvents) != 2 {
			t.Errorf("expected 2 events, got %d", len(store.appendEvents))
		}

		// 验证事件已标记为提交
		if len(agg.GetUncommittedEvents()) != 0 {
			t.Errorf("expected 0 uncommitted events after Save, got %d", len(agg.GetUncommittedEvents()))
		}
	})

	t.Run("保存没有未提交事件的聚合（不调用AppendEvents）", func(t *testing.T) {
		store := &mockEventStore{}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		agg := NewTestAggregate(1)

		// 保存
		err := repo.Save(context.Background(), agg)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// 验证未调用 AppendEvents
		if store.appendCalled {
			t.Error("AppendEvents should not have been called for empty uncommitted events")
		}
	})

	t.Run("expectedVersion计算正确（已有版本的聚合）", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents: []domain.IDomainEvent{
				&TestEvent{eventType: "OldEvent1", data: "old1"},
				&TestEvent{eventType: "OldEvent2", data: "old2"},
				&TestEvent{eventType: "OldEvent3", data: "old3"},
			},
			restoreVersion: 3,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		// 加载已有聚合（版本=3）
		agg, _ := repo.GetByID(context.Background(), 1)

		// 添加2个新事件（版本变为5）
		evt1 := &TestEvent{eventType: "NewEvent1", data: "new1"}
		evt2 := &TestEvent{eventType: "NewEvent2", data: "new2"}
		agg.ApplyAndRecord(evt1)
		agg.ApplyAndRecord(evt2)

		// 保存
		store.appendCalled = false // 重置
		err := repo.Save(context.Background(), agg)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// 验证 expectedVersion 计算
		// currentVersion=5, eventCount=2, expectedVersion=3
		if store.appendExpectedVersion != 3 {
			t.Errorf("expected expectedVersion 3, got %d", store.appendExpectedVersion)
		}
	})

	t.Run("版本号小于事件数量返回错误（检测ApplyEvent未递增版本）", func(t *testing.T) {
		store := &mockEventStore{}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		agg := NewTestAggregate(1)

		// 添加事件但不应用（破坏版本一致性）
		evt := &TestEvent{eventType: "Event1", data: "data1"}
		agg.AddDomainEvent(evt)
		// version=0, eventCount=1, 触发错误

		err := repo.Save(context.Background(), agg)
		if err == nil {
			t.Fatal("expected error for currentVersion < eventCount, got nil")
		}

		if !errors.Is(err, errors.New("version calculation error")) &&
			err.Error() != "version calculation error: currentVersion(0) < eventCount(1). This usually indicates that the ApplyEvent implementation of aggregate type TestAggregate does not correctly increment the version. Please check the implementation and ensure that each ApplyEvent call executes version++" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("AppendEvents失败返回错误", func(t *testing.T) {
		store := &mockEventStore{
			appendError: errors.New("append failed"),
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		agg := NewTestAggregate(1)
		evt := &TestEvent{eventType: "Event1", data: "data1"}
		agg.ApplyAndRecord(evt)

		err := repo.Save(context.Background(), agg)
		if err == nil {
			t.Fatal("expected error from AppendEvents, got nil")
		}

		if err.Error() != "append failed" {
			t.Errorf("unexpected error: %v", err)
		}

		// 验证事件未被标记为提交
		if len(agg.GetUncommittedEvents()) != 1 {
			t.Error("events should not be marked as committed when AppendEvents fails")
		}
	})
}

// TestRepositoryGetByID 测试通过ID获取聚合
func TestRepositoryGetByID(t *testing.T) {
	t.Run("成功加载聚合", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents: []domain.IDomainEvent{
				&TestEvent{eventType: "Event1", data: "data1"},
				&TestEvent{eventType: "Event2", data: "data2"},
			},
			restoreVersion: 2,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		agg, err := repo.GetByID(context.Background(), 100)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}

		if agg.GetID() != 100 {
			t.Errorf("expected ID 100, got %d", agg.GetID())
		}

		if agg.GetVersion() != 2 {
			t.Errorf("expected version 2, got %d", agg.GetVersion())
		}

		if agg.Data != "data2" {
			t.Errorf("expected Data 'data2', got '%s'", agg.Data)
		}

		if !store.restoreCalled {
			t.Error("RestoreAggregate should have been called")
		}
	})

	t.Run("RestoreAggregate失败返回错误", func(t *testing.T) {
		store := &mockEventStore{
			restoreError: errors.New("restore failed"),
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		_, err := repo.GetByID(context.Background(), 100)
		if err == nil {
			t.Fatal("expected error from RestoreAggregate, got nil")
		}

		if err.Error() != "restore failed" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("加载不存在的聚合返回空聚合", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents:  []domain.IDomainEvent{},
			restoreVersion: 0,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		agg, err := repo.GetByID(context.Background(), 999)
		if err != nil {
			t.Fatalf("GetByID should not fail for non-existent aggregate: %v", err)
		}

		if agg.GetVersion() != 0 {
			t.Errorf("expected version 0 for new aggregate, got %d", agg.GetVersion())
		}
	})
}

// TestRepositoryExists 测试检查聚合是否存在
func TestRepositoryExists(t *testing.T) {
	t.Run("聚合存在返回true", func(t *testing.T) {
		store := &mockEventStore{
			exists: true,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		exists, err := repo.Exists(context.Background(), 100)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if !exists {
			t.Error("expected exists=true, got false")
		}

		if !store.existsCalled {
			t.Error("store.Exists should have been called")
		}
	})

	t.Run("聚合不存在返回false", func(t *testing.T) {
		store := &mockEventStore{
			exists: false,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		exists, err := repo.Exists(context.Background(), 999)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}

		if exists {
			t.Error("expected exists=false, got true")
		}
	})

	t.Run("Exists错误返回错误", func(t *testing.T) {
		store := &mockEventStore{
			existsError: errors.New("exists check failed"),
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		_, err := repo.Exists(context.Background(), 100)
		if err == nil {
			t.Fatal("expected error from Exists, got nil")
		}

		if err.Error() != "exists check failed" {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestRepositoryGetAggregateVersion 测试获取聚合版本
func TestRepositoryGetAggregateVersion(t *testing.T) {
	t.Run("获取存在的聚合版本", func(t *testing.T) {
		store := &mockEventStore{
			version: 5,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		version, err := repo.GetAggregateVersion(context.Background(), 100)
		if err != nil {
			t.Fatalf("GetAggregateVersion failed: %v", err)
		}

		if version != 5 {
			t.Errorf("expected version 5, got %d", version)
		}

		if !store.getVersionCalled {
			t.Error("store.GetAggregateVersion should have been called")
		}
	})

	t.Run("不存在的聚合返回版本0", func(t *testing.T) {
		store := &mockEventStore{
			version: 0,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		version, err := repo.GetAggregateVersion(context.Background(), 999)
		if err != nil {
			t.Fatalf("GetAggregateVersion failed: %v", err)
		}

		if version != 0 {
			t.Errorf("expected version 0 for non-existent aggregate, got %d", version)
		}
	})

	t.Run("GetAggregateVersion错误返回错误", func(t *testing.T) {
		store := &mockEventStore{
			versionError: errors.New("version check failed"),
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		_, err := repo.GetAggregateVersion(context.Background(), 100)
		if err == nil {
			t.Fatal("expected error from GetAggregateVersion, got nil")
		}

		if err.Error() != "version check failed" {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestRepositoryConcurrencyScenario 测试并发场景
func TestRepositoryConcurrencyScenario(t *testing.T) {
	t.Run("模拟乐观锁冲突检测", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents: []domain.IDomainEvent{
				&TestEvent{eventType: "InitialEvent", data: "initial"},
			},
			restoreVersion: 1,
		}
		repo, _ := NewEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			NewTestAggregate,
			store,
		)

		// 事务1加载聚合（版本=1）
		agg1, _ := repo.GetByID(context.Background(), 1)

		// 事务2加载聚合（版本=1）
		agg2, _ := repo.GetByID(context.Background(), 1)

		// 事务1修改并保存
		evt1 := &TestEvent{eventType: "Transaction1Event", data: "tx1"}
		agg1.ApplyAndRecord(evt1)
		err := repo.Save(context.Background(), agg1)
		if err != nil {
			t.Fatalf("Transaction1 Save failed: %v", err)
		}

		// 验证事务1的expectedVersion=1
		if store.appendExpectedVersion != 1 {
			t.Errorf("Transaction1: expected expectedVersion 1, got %d", store.appendExpectedVersion)
		}

		// 事务2尝试修改并保存（应该失败，因为版本已变）
		evt2 := &TestEvent{eventType: "Transaction2Event", data: "tx2"}
		agg2.ApplyAndRecord(evt2)

		// 模拟并发冲突
		store.appendError = errors.New("concurrency conflict: expected version 1, actual version 2")
		err = repo.Save(context.Background(), agg2)
		if err == nil {
			t.Fatal("Transaction2 should fail due to optimistic lock conflict")
		}

		// 验证事务2的expectedVersion也是1（但与当前版本冲突）
		if store.appendExpectedVersion != 1 {
			t.Errorf("Transaction2: expected expectedVersion 1, got %d", store.appendExpectedVersion)
		}
	})
}
