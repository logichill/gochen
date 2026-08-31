package outbox

import (
	"gochen/eventing/internal/testutil"
	"testing"

	"gochen/testkit/require"

	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
)

type testEventPayload struct {
	Value int `json:"value"`
}

func newTestRegistry(tb testing.TB) *registry.Registry {
	tb.Helper()
	reg := registry.NewRegistry()
	require.NoError(tb, reg.Register("TestEvent", func() any { return &testEventPayload{} }))
	return reg
}

func newTestUpgraders() *upcast.UpgraderRegistry {
	return upcast.NewUpgraderRegistry()
}

// newTestEvent aggregateID：对象/实体标识。
//
// 参数：
// - version：版本号（类型：uint64）
// - id：对象/实体标识
// - payload：属性/参数集合（类型：map[string]any）
//
// 返回：
// - result：测试返回值（类型：eventing.Event[int64]）
func newTestEvent(aggregateID int64, version uint64, id string, payload map[string]any) eventing.Event[int64] {
	if payload == nil {
		payload = make(map[string]any)
	}

	evt := testutil.NewEvent[int64](aggregateID, "TestAggregate", "TestEvent", version, payload)
	evt.ID = id
	evt.GetMetadata().Set("source", "unit_test")
	return *evt
}
