package eventsourced

import (
	"context"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
)

// mockEventStore 是用于单元测试的最小 IEventStore[int64] 实现。
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

// AppendEvents 向事件存储追加事件。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateID：对象/实体标识
// - events：事件列表（待追加/发布）（类型：[]domain.IDomainEvent）
// - expectedVersion：期望版本（用于乐观并发控制）（类型：uint64）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *mockEventStore) AppendEvents(ctx context.Context, aggregateID int64, events []domain.IDomainEvent, expectedVersion uint64) error {
	m.appendCalled = true
	m.appendEvents = events
	m.appendExpectedVersion = expectedVersion
	return m.appendError
}

// RestoreAggregate ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - aggregate：聚合实例
//
// 返回：
// - result1：返回结果（类型：*deventsourced.RestoreResult）
// - err：错误信息（nil 表示成功）
func (m *mockEventStore) RestoreAggregate(ctx context.Context, aggregate deventsourced.IEventSourcedAggregate[int64]) (*deventsourced.RestoreResult, error) {
	m.restoreCalled = true
	if m.restoreError != nil {
		return nil, m.restoreError
	}

	for _, evt := range m.restoreEvents {
		if err := aggregate.ApplyEvent(evt); err != nil {
			return nil, err
		}
	}

	version := m.restoreVersion
	if version == 0 {
		version = aggregate.GetVersion()
	}

	return &deventsourced.RestoreResult{
		Version:      version,
		EventCount:   len(m.restoreEvents),
		Exists:       version > 0,
		FromSnapshot: false,
	}, nil
}

// Exists 判断对象是否存在。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateID：对象/实体标识
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (m *mockEventStore) Exists(ctx context.Context, aggregateID int64) (bool, error) {
	m.existsCalled = true
	return m.exists, m.existsError
}

// GetAggregateVersion 从存储中查询数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - aggregateID：对象/实体标识
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (m *mockEventStore) GetAggregateVersion(ctx context.Context, aggregateID int64) (uint64, error) {
	m.getVersionCalled = true
	return m.version, m.versionError
}

var _ deventsourced.IDomainEventStore[int64] = (*mockEventStore)(nil)
