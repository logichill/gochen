package projection

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
)

// TestMemoryCheckpointStore_SaveAndLoad 验证 MemoryCheckpointStore SaveAndLoad。
func TestMemoryCheckpointStore_SaveAndLoad(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	checkpoint := NewCheckpoint("test-projection", 100, "event-123", time.Now())

	// 保存
	err := store.Save(ctx, checkpoint)
	require.NoError(t, err)

	// 加载
	loaded, err := store.Load(ctx, "test-projection")
	require.NoError(t, err)
	assert.Equal(t, checkpoint.ProjectionName, loaded.ProjectionName)
	assert.Equal(t, checkpoint.Position, loaded.Position)
	assert.Equal(t, checkpoint.LastEventID, loaded.LastEventID)
}

// TestMemoryCheckpointStore_LoadNotFound 验证 MemoryCheckpointStore LoadNotFound。
func TestMemoryCheckpointStore_LoadNotFound(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	_, err := store.Load(ctx, "non-existent")
	assert.True(t, errors.Is(err, errors.NotFound), "expected NotFound, got: %v", err)
}

// TestMemoryCheckpointStore_SaveInvalid 验证 MemoryCheckpointStore SaveInvalid。
func TestMemoryCheckpointStore_SaveInvalid(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	// 空检查点
	err := store.Save(ctx, nil)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got: %v", err)

	// 无效检查点
	invalid := &Checkpoint{ProjectionName: "", Position: 10}
	err = store.Save(ctx, invalid)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got: %v", err)
}

// TestMemoryCheckpointStore_Update 验证 MemoryCheckpointStore Update。
func TestMemoryCheckpointStore_Update(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	// 初始保存
	checkpoint1 := NewCheckpoint("test", 100, "event-100", time.Now())
	err := store.Save(ctx, checkpoint1)
	require.NoError(t, err)

	// 更新（覆盖）
	checkpoint2 := NewCheckpoint("test", 200, "event-200", time.Now())
	err = store.Save(ctx, checkpoint2)
	require.NoError(t, err)

	// 验证更新
	loaded, err := store.Load(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, int64(200), loaded.Position)
	assert.Equal(t, "event-200", loaded.LastEventID)
}

// TestMemoryCheckpointStore_Delete 验证 MemoryCheckpointStore Delete。
func TestMemoryCheckpointStore_Delete(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	// 保存
	checkpoint := NewCheckpoint("test", 100, "event-100", time.Now())
	err := store.Save(ctx, checkpoint)
	require.NoError(t, err)

	// 删除
	err = store.Delete(ctx, "test")
	require.NoError(t, err)

	// 验证删除
	_, err = store.Load(ctx, "test")
	assert.True(t, errors.Is(err, errors.NotFound), "expected NotFound, got: %v", err)

	// 删除不存在的检查点（不应该报错）
	err = store.Delete(ctx, "non-existent")
	assert.NoError(t, err)
}

// TestMemoryCheckpointStore_Clear 验证 MemoryCheckpointStore Clear。
func TestMemoryCheckpointStore_Clear(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	// 保存多个检查点
	for i := 0; i < 5; i++ {
		checkpoint := NewCheckpoint("test-"+string(rune('A'+i)), int64(i*100), "event", time.Now())
		err := store.Save(ctx, checkpoint)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, store.Count())

	// 清空
	store.Clear()
	assert.Equal(t, 0, store.Count())
}

// TestMemoryCheckpointStore_ConcurrentAccess 验证 MemoryCheckpointStore ConcurrentAccess。
func TestMemoryCheckpointStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 100

	// 并发写入
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			checkpoint := NewCheckpoint("test", int64(idx), "event", time.Now())
			_ = store.Save(ctx, checkpoint)
		}(i)
	}

	wg.Wait()

	// 验证最终状态
	loaded, err := store.Load(ctx, "test")
	require.NoError(t, err)
	assert.NotNil(t, loaded)
	assert.GreaterOrEqual(t, loaded.Position, int64(0))
	assert.LessOrEqual(t, loaded.Position, int64(numGoroutines-1))
}

// BenchmarkMemoryCheckpointStore_Save 用于评估 MemoryCheckpointStore Save 的性能。
func BenchmarkMemoryCheckpointStore_Save(b *testing.B) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()
	checkpoint := NewCheckpoint("test", 100, "event-123", time.Now())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Save(ctx, checkpoint)
	}
}

// BenchmarkMemoryCheckpointStore_Load 用于评估 MemoryCheckpointStore Load 的性能。
func BenchmarkMemoryCheckpointStore_Load(b *testing.B) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()
	checkpoint := NewCheckpoint("test", 100, "event-123", time.Now())
	_ = store.Save(ctx, checkpoint)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Load(ctx, "test")
	}
}

// BenchmarkMemoryCheckpointStore_ConcurrentSave 用于评估 MemoryCheckpointStore ConcurrentSave 的性能。
func BenchmarkMemoryCheckpointStore_ConcurrentSave(b *testing.B) {
	store := NewMemoryCheckpointStore()
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		checkpoint := NewCheckpoint("test", 100, "event-123", time.Now())
		for pb.Next() {
			_ = store.Save(ctx, checkpoint)
		}
	})
}
