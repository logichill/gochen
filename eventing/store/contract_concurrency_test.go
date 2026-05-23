package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing"
)

// TestContract_MemoryEventStore_AppendEvents_ConcurrencyErrorCode 验证 Contract MemoryEventStore AppendEvents ConcurrencyErrorCode。
func TestContract_MemoryEventStore_AppendEvents_ConcurrencyErrorCode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore()

	aggregateID := int64(1)
	aggregateType := "TestAggregate"

	// 第一次追加成功（expectedVersion=0）
	e1 := eventing.NewEvent[int64](aggregateID, aggregateType, "Created", 1, nil)
	err := store.AppendEvents(ctx, aggregateID, []eventing.IStorableEvent[int64]{e1}, 0)
	require.NoError(t, err, "first append should succeed")

	// 第二次使用相同的 expectedVersion=0 追加（应该冲突）
	// 注意：事件版本设为 1（即 expectedVersion + 1），以通过事件版本序列校验，
	// 让测试能到达 expectedVersion vs currentVersion 的并发检查
	e2 := eventing.NewEvent[int64](aggregateID, aggregateType, "Updated", 1, nil)
	err = store.AppendEvents(ctx, aggregateID, []eventing.IStorableEvent[int64]{e2}, 0)

	// 验证错误码
	require.Error(t, err, "second append with stale version should fail")
	require.True(t, errors.Is(err, errors.Concurrency), "error should be Concurrency, got: %v", err)

	// 验证错误详情
	var appErr *errors.AppError
	require.ErrorAs(t, err, &appErr, "error should be *errors.AppError")

	details := appErr.Details()
	require.Contains(t, details, "aggregate_id", "details should contain aggregate_id")
	require.Contains(t, details, "expected_version", "details should contain expected_version")
	require.Contains(t, details, "actual_version", "details should contain actual_version")

	// 验证详情值
	require.Equal(t, aggregateID, details["aggregate_id"], "aggregate_id should match")
	require.Equal(t, uint64(0), details["expected_version"], "expected_version should be 0")
	require.Equal(t, uint64(1), details["actual_version"], "actual_version should be 1")
}

// TestContract_MemoryEventStore_AppendEvents_CorrectVersionSucceeds 验证 Contract MemoryEventStore AppendEvents CorrectVersionSucceeds。
func TestContract_MemoryEventStore_AppendEvents_CorrectVersionSucceeds(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore()

	aggregateID := int64(1)
	aggregateType := "TestAggregate"

	// 第一次追加（expectedVersion=0）
	e1 := eventing.NewEvent[int64](aggregateID, aggregateType, "Created", 1, nil)
	err := store.AppendEvents(ctx, aggregateID, []eventing.IStorableEvent[int64]{e1}, 0)
	require.NoError(t, err, "first append should succeed")

	// 第二次使用正确的 expectedVersion=1 追加
	e2 := eventing.NewEvent[int64](aggregateID, aggregateType, "Updated", 2, nil)
	err = store.AppendEvents(ctx, aggregateID, []eventing.IStorableEvent[int64]{e2}, 1)
	require.NoError(t, err, "second append with correct version should succeed")

	// 验证版本
	version, err := store.GetAggregateVersion(ctx, aggregateID)
	require.NoError(t, err)
	require.Equal(t, uint64(2), version, "version should be 2 after two appends")
}
