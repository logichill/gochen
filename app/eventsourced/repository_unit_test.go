package eventsourced

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/domain"
	deventsourced "gochen/domain/eventsourced"
	gerrors "gochen/errors"
)

// 测试用的简单事件
type TestEvent struct {
	eventType string
	data      string
}

// EventType 返回事件类型标识。
//
// 返回：
// - result：文本结果
func (e *TestEvent) EventType() string { return e.eventType }

// 测试用的聚合（通过自动路由 handler 更新状态）。
type TestAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Data string
}

// NewTestAggregate 创建并返回TestAggregate实例。
//
// 参数：
// - id：对象/实体标识
//
// 返回：
// - result：返回的实例（类型：*TestAggregate）
func NewTestAggregate(id int64) *TestAggregate {
	a := &TestAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "TestAggregate")
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

// factory 返回聚合实例，由 Repository 在加载路径显式注入预编译 metadata。
type autoBindAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Data string
}

// newAutoBindAggregate id：对象/实体标识。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*autoBindAggregate）
func newAutoBindAggregate(id int64) *autoBindAggregate {
	a := &autoBindAggregate{}
	agg, err := deventsourced.InitAggregate[int64](testMetadataRegistry, a, id, "AutoBindAggregate")
	if err != nil {
		panic(err)
	}
	a.EventSourcedAggregate = agg
	return a
}

// ApplyTestEvent e：实体对象。
//
// 参数：
func (a *autoBindAggregate) ApplyTestEvent(e *TestEvent) { a.Data = e.data }

// TestNewEventSourcedRepository 验证 NewEventSourcedRepository。
func TestNewEventSourcedRepository(t *testing.T) {
	store := &mockEventStore{}
	type namedAggregateFactory func(int64) *TestAggregate

	t.Run("成功创建仓储", func(t *testing.T) {
		repo, err := newTestEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			&TestAggregate{},
			AdaptAggregateFactory(NewTestAggregate),
			store,
		)
		require.NoError(t, err)
		require.NotNil(t, repo)
		require.Equal(t, "TestAggregate", repo.aggregateType)
	})

	t.Run("未显式预注册 metadata 时自动 ensure", func(t *testing.T) {
		repo, err := newTestEventSourcedRepository[*autoBindAggregate](
			"AutoBindAggregateNoWarmup",
			&autoBindAggregate{},
			AdaptAggregateFactory(func(id int64) *autoBindAggregate {
				return &autoBindAggregate{
					EventSourcedAggregate: deventsourced.NewEventSourcedAggregate[int64](id, "AutoBindAggregateNoWarmup"),
				}
			}),
			store,
		)
		require.NoError(t, err)
		require.NotNil(t, repo)
	})

	t.Run("支持命名纯工厂类型通过适配器构造仓储", func(t *testing.T) {
		var factory namedAggregateFactory = NewTestAggregate
		repo, err := newTestEventSourcedRepository[*TestAggregate](
			"TestAggregate",
			&TestAggregate{},
			AdaptAggregateFactory(factory),
			store,
		)
		require.NoError(t, err)
		require.NotNil(t, repo)
	})

	t.Run("aggregateType为空返回错误", func(t *testing.T) {
		_, err := newTestEventSourcedRepository[*TestAggregate]("", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.Error(t, err)
		require.True(t, gerrors.Is(err, gerrors.InvalidInput))
		require.True(t, strings.Contains(err.Error(), "aggregate type cannot be empty"))
	})

	t.Run("factory为nil返回错误", func(t *testing.T) {
		var factory func(int64) (*TestAggregate, error)
		_, err := newTestEventSourcedRepository[*TestAggregate, int64]("TestAggregate", &TestAggregate{}, factory, store)
		require.Error(t, err)
		require.True(t, gerrors.Is(err, gerrors.InvalidInput))
		require.True(t, strings.Contains(err.Error(), "aggregate factory cannot be nil"))
	})

	t.Run("store为nil返回错误", func(t *testing.T) {
		_, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), nil)
		require.Error(t, err)
		require.True(t, gerrors.Is(err, gerrors.InvalidInput))
		require.True(t, strings.Contains(err.Error(), "event store cannot be nil"))
	})
}

// TestRepositorySave 验证 RepositorySave。
func TestRepositorySave(t *testing.T) {
	t.Run("保存有未提交事件的聚合", func(t *testing.T) {
		store := &mockEventStore{}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg := NewTestAggregate(1)
		evt1 := &TestEvent{eventType: "Event1", data: "data1"}
		evt2 := &TestEvent{eventType: "Event2", data: "data2"}

		require.NoError(t, agg.ApplyAndRecord(evt1))
		require.NoError(t, agg.ApplyAndRecord(evt2))

		require.NoError(t, repo.Save(context.Background(), agg))
		require.True(t, store.appendCalled)
		require.Equal(t, uint64(0), store.appendExpectedVersion)
		require.Len(t, store.appendEvents, 2)
		require.Len(t, agg.GetUncommittedEvents(), 0)
	})

	t.Run("保存没有未提交事件的聚合（不调用AppendEvents）", func(t *testing.T) {
		store := &mockEventStore{}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg := NewTestAggregate(1)
		require.NoError(t, repo.Save(context.Background(), agg))
		require.False(t, store.appendCalled)
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
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg, err := repo.Get(context.Background(), 1)
		require.NoError(t, err)

		require.NoError(t, agg.ApplyAndRecord(&TestEvent{eventType: "NewEvent1", data: "new1"}))
		require.NoError(t, agg.ApplyAndRecord(&TestEvent{eventType: "NewEvent2", data: "new2"}))

		store.appendCalled = false
		require.NoError(t, repo.Save(context.Background(), agg))
		require.True(t, store.appendCalled)
		require.Equal(t, uint64(3), store.appendExpectedVersion)
	})

	t.Run("显式 expectedVersion 由聚合维护", func(t *testing.T) {
		store := &mockEventStore{}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg := NewTestAggregate(1)
		agg.SetVersion(7)
		require.NoError(t, agg.RecordUncommittedEvent(&TestEvent{eventType: "Event1", data: "data1"}))

		require.NoError(t, repo.Save(context.Background(), agg))
		require.True(t, store.appendCalled)
		require.Equal(t, uint64(7), store.appendExpectedVersion)
	})

	t.Run("AppendEvents失败返回错误", func(t *testing.T) {
		store := &mockEventStore{
			appendError: gerrors.New("append failed"),
		}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg := NewTestAggregate(1)
		require.NoError(t, agg.ApplyAndRecord(&TestEvent{eventType: "Event1", data: "data1"}))

		err = repo.Save(context.Background(), agg)
		require.Error(t, err)
		require.Equal(t, "append failed", err.Error())
		require.Len(t, agg.GetUncommittedEvents(), 1)
	})
}

// TestRepositoryGet 验证 RepositoryGet。
func TestRepositoryGet(t *testing.T) {
	t.Run("成功加载聚合", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents: []domain.IDomainEvent{
				&TestEvent{eventType: "Event1", data: "data1"},
				&TestEvent{eventType: "Event2", data: "data2"},
			},
			restoreVersion: 2,
		}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg, err := repo.Get(context.Background(), 100)
		require.NoError(t, err)
		require.Equal(t, int64(100), agg.GetID())
		require.Equal(t, uint64(2), agg.GetVersion())
		require.Equal(t, "data2", agg.Data)
		require.True(t, store.restoreCalled)
	})

	t.Run("factory未手工绑定元数据也能正确回放（Repository自动注入metadata）", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents: []domain.IDomainEvent{
				&TestEvent{eventType: "Event1", data: "data1"},
				&TestEvent{eventType: "Event2", data: "data2"},
			},
			restoreVersion: 2,
		}
		repo, err := newTestEventSourcedRepository[*autoBindAggregate]("AutoBindAggregate", &autoBindAggregate{}, AdaptAggregateFactory(newAutoBindAggregate), store)
		require.NoError(t, err)

		agg, err := repo.Get(context.Background(), 100)
		require.NoError(t, err)
		require.Equal(t, "data2", agg.Data)
		require.Equal(t, uint64(2), agg.GetVersion())
	})

	t.Run("RestoreAggregate失败返回错误", func(t *testing.T) {
		store := &mockEventStore{
			restoreError: gerrors.New("restore failed"),
		}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		_, err = repo.Get(context.Background(), 100)
		require.Error(t, err)
		require.Equal(t, "restore failed", err.Error())
	})

	t.Run("加载不存在的聚合返回NotFound", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents:  []domain.IDomainEvent{},
			restoreVersion: 0,
		}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		_, err = repo.Get(context.Background(), 999)
		require.Error(t, err)
		require.True(t, gerrors.Is(err, gerrors.NotFound))
	})
}

// TestRepositoryExists 验证 RepositoryExists。
func TestRepositoryExists(t *testing.T) {
	t.Run("聚合存在返回true", func(t *testing.T) {
		store := &mockEventStore{exists: true}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		exists, err := repo.Exists(context.Background(), 100)
		require.NoError(t, err)
		require.True(t, exists)
		require.True(t, store.existsCalled)
	})

	t.Run("聚合不存在返回false", func(t *testing.T) {
		store := &mockEventStore{exists: false}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		exists, err := repo.Exists(context.Background(), 999)
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("Exists错误返回错误", func(t *testing.T) {
		store := &mockEventStore{existsError: gerrors.New("exists check failed")}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		_, err = repo.Exists(context.Background(), 100)
		require.Error(t, err)
		require.Equal(t, "exists check failed", err.Error())
	})
}

// TestRepositoryGetAggregateVersion 验证 RepositoryGetAggregateVersion。
func TestRepositoryGetAggregateVersion(t *testing.T) {
	t.Run("获取存在的聚合版本", func(t *testing.T) {
		store := &mockEventStore{version: 5}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		version, err := repo.GetAggregateVersion(context.Background(), 100)
		require.NoError(t, err)
		require.Equal(t, uint64(5), version)
		require.True(t, store.getVersionCalled)
	})

	t.Run("不存在的聚合返回版本0", func(t *testing.T) {
		store := &mockEventStore{version: 0}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		version, err := repo.GetAggregateVersion(context.Background(), 999)
		require.NoError(t, err)
		require.Equal(t, uint64(0), version)
	})

	t.Run("GetAggregateVersion错误返回错误", func(t *testing.T) {
		store := &mockEventStore{versionError: gerrors.New("version check failed")}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		_, err = repo.GetAggregateVersion(context.Background(), 100)
		require.Error(t, err)
		require.Equal(t, "version check failed", err.Error())
	})
}

// TestRepositoryConcurrencyScenario 验证 RepositoryConcurrencyScenario。
func TestRepositoryConcurrencyScenario(t *testing.T) {
	t.Run("模拟乐观锁冲突检测", func(t *testing.T) {
		store := &mockEventStore{
			restoreEvents: []domain.IDomainEvent{
				&TestEvent{eventType: "InitialEvent", data: "initial"},
			},
			restoreVersion: 1,
		}
		repo, err := newTestEventSourcedRepository[*TestAggregate]("TestAggregate", &TestAggregate{}, AdaptAggregateFactory(NewTestAggregate), store)
		require.NoError(t, err)

		agg1, err := repo.Get(context.Background(), 1)
		require.NoError(t, err)
		agg2, err := repo.Get(context.Background(), 1)
		require.NoError(t, err)

		require.NoError(t, agg1.ApplyAndRecord(&TestEvent{eventType: "Transaction1Event", data: "tx1"}))
		require.NoError(t, repo.Save(context.Background(), agg1))
		require.Equal(t, uint64(1), store.appendExpectedVersion)

		require.NoError(t, agg2.ApplyAndRecord(&TestEvent{eventType: "Transaction2Event", data: "tx2"}))

		store.appendError = gerrors.New("concurrency conflict: expected version 1, actual version 2")
		err = repo.Save(context.Background(), agg2)
		require.Error(t, err)
		require.Equal(t, uint64(1), store.appendExpectedVersion)
	})
}

var _ domain.IDomainEvent = (*TestEvent)(nil)
