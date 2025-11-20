package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCache_BasicOperations 测试基本操作
func TestCache_BasicOperations(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     time.Minute,
	})

	// 测试 Set 和 Get
	cache.Set("key1", 100)
	value, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, 100, value)

	// 测试不存在的 key
	_, found = cache.Get("nonexistent")
	assert.False(t, found)

	// 测试 Delete
	deleted := cache.Delete("key1")
	assert.True(t, deleted)

	_, found = cache.Get("key1")
	assert.False(t, found)

	// 测试重复删除
	deleted = cache.Delete("key1")
	assert.False(t, deleted)
}

// TestCache_Update 测试更新操作
func TestCache_Update(t *testing.T) {
	cache := New[int64, string](Config{
		Name:    "test",
		MaxSize: 100,
	})

	// 设置初始值
	cache.Set(1, "first")
	value, found := cache.Get(1)
	require.True(t, found)
	assert.Equal(t, "first", value)

	// 更新值
	cache.Set(1, "second")
	value, found = cache.Get(1)
	require.True(t, found)
	assert.Equal(t, "second", value)

	// Size 应该还是 1
	assert.Equal(t, 1, cache.Size())
}

// TestCache_LRUEviction 测试 LRU 驱逐
func TestCache_LRUEviction(t *testing.T) {
	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 3, // 最多 3 个条目
	})

	// 添加 3 个条目
	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	assert.Equal(t, 3, cache.Size())

	// 访问 key=1，使其成为最近使用的
	_, found := cache.Get(1)
	assert.True(t, found)

	// 添加第 4 个条目，应该驱逐 key=2（最久未使用的）
	cache.Set(4, "four")

	assert.Equal(t, 3, cache.Size())

	// key=2 应该被驱逐
	_, found = cache.Get(2)
	assert.False(t, found)

	// key=1, 3, 4 应该还在
	_, found = cache.Get(1)
	assert.True(t, found)
	_, found = cache.Get(3)
	assert.True(t, found)
	_, found = cache.Get(4)
	assert.True(t, found)

	// 验证驱逐统计
	stats := cache.Stats()
	assert.Equal(t, int64(1), stats.Evictions)
}

// TestCache_TTLExpiration 测试 TTL 过期
func TestCache_TTLExpiration(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     100 * time.Millisecond,
	})

	// 设置值
	cache.Set("key1", 100)

	// 立即获取应该成功
	value, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, 100, value)

	// 等待过期
	time.Sleep(150 * time.Millisecond)

	// 过期后获取应该失败
	_, found = cache.Get("key1")
	assert.False(t, found)

	// 验证过期统计
	stats := cache.Stats()
	assert.Equal(t, int64(1), stats.Expires)
}

// TestCache_TTLRefreshOnAccess 测试访问刷新 TTL
func TestCache_TTLRefreshOnAccess(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     200 * time.Millisecond,
	})

	cache.Set("key1", 100)

	// 在过期前持续访问
	for i := 0; i < 3; i++ {
		time.Sleep(100 * time.Millisecond)
		value, found := cache.Get("key1")
		assert.True(t, found, "iteration %d", i)
		assert.Equal(t, 100, value)
	}

	// 总共过了 300ms，但因为持续访问，不应该过期
	_, found := cache.Get("key1")
	assert.True(t, found)
}

// TestCache_CleanExpired 测试手动清理过期条目
func TestCache_CleanExpired(t *testing.T) {
	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     100 * time.Millisecond,
	})

	// 添加多个条目
	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	assert.Equal(t, 3, cache.Size())

	// 等待过期
	time.Sleep(150 * time.Millisecond)

	// 手动清理
	cleaned := cache.CleanExpired()
	assert.Equal(t, 3, cleaned)
	assert.Equal(t, 0, cache.Size())
}

// TestCache_Clear 测试清空缓存
func TestCache_Clear(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
	})

	// 添加多个条目
	for i := 0; i < 10; i++ {
		cache.Set(string(rune('a'+i)), i)
	}

	assert.Equal(t, 10, cache.Size())

	// 清空
	cache.Clear()

	assert.Equal(t, 0, cache.Size())

	// 所有 key 都不应该存在
	for i := 0; i < 10; i++ {
		_, found := cache.Get(string(rune('a' + i)))
		assert.False(t, found)
	}
}

// TestCache_Stats 测试统计信息
func TestCache_Stats(t *testing.T) {
	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 2,
		TTL:     100 * time.Millisecond,
	})

	// 初始统计
	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, int64(0), stats.Evictions)

	// 设置两个值
	cache.Set(1, "one")
	cache.Set(2, "two")

	// 命中一次
	_, found := cache.Get(1)
	assert.True(t, found)

	// 未命中一次
	_, found = cache.Get(999)
	assert.False(t, found)

	// 触发驱逐
	cache.Set(3, "three")

	// 等待过期
	time.Sleep(150 * time.Millisecond)
	_, _ = cache.Get(1) // 触发过期检查

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Hits)   // 第一次 Get(1)
	assert.Equal(t, int64(2), stats.Misses) // Get(999) + 过期的 Get(1)
	assert.Equal(t, int64(1), stats.Evictions)
	assert.Equal(t, int64(1), stats.Expires)
}

// TestCache_HitRate 测试命中率计算
func TestCache_HitRate(t *testing.T) {
	cache := New[int, int](Config{
		Name:    "test",
		MaxSize: 100,
	})

	// 初始命中率应该是 0
	assert.Equal(t, 0.0, cache.HitRate())

	cache.Set(1, 100)

	// 3 次命中
	for i := 0; i < 3; i++ {
		_, found := cache.Get(1)
		assert.True(t, found)
	}

	// 1 次未命中
	_, found := cache.Get(999)
	assert.False(t, found)

	// 命中率应该是 75% (3/4)
	assert.InDelta(t, 0.75, cache.HitRate(), 0.01)
}

// TestCache_OnEvict 测试驱逐回调
func TestCache_OnEvict(t *testing.T) {
	evicted := make(map[int]string)

	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 2,
		OnEvict: func(key, value any) {
			evicted[key.(int)] = value.(string)
		},
	})

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three") // 应该驱逐 key=1

	// 验证回调被调用
	assert.Equal(t, 1, len(evicted))
	assert.Equal(t, "one", evicted[1])

	// 手动删除
	cache.Delete(2)

	// 验证回调被调用
	assert.Equal(t, 2, len(evicted))
	assert.Equal(t, "two", evicted[2])

	// Clear 也应该触发回调
	evicted = make(map[int]string)
	cache.Clear()
	assert.Equal(t, 1, len(evicted))
	assert.Equal(t, "three", evicted[3])
}

// TestCache_ConcurrentAccess 测试并发访问
func TestCache_ConcurrentAccess(t *testing.T) {
	cache := New[int, int](Config{
		Name:    "test",
		MaxSize: 1000,
	})

	// 并发写入
	const goroutines = 10
	const iterations = 100

	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			for i := 0; i < iterations; i++ {
				key := id*iterations + i
				cache.Set(key, key*2)
			}
			done <- true
		}(g)
	}

	// 等待所有 goroutine 完成
	for g := 0; g < goroutines; g++ {
		<-done
	}

	// 验证所有值都被正确写入
	for g := 0; g < goroutines; g++ {
		for i := 0; i < iterations; i++ {
			key := g*iterations + i
			value, found := cache.Get(key)
			assert.True(t, found)
			assert.Equal(t, key*2, value)
		}
	}
}

// TestCache_AggregateUseCase 测试聚合根缓存用例
func TestCache_AggregateUseCase(t *testing.T) {
	// 模拟聚合根
	type UserAggregate struct {
		ID      int64
		Name    string
		Version int
	}

	cache := New[int64, *UserAggregate](Config{
		Name:    "user_aggregate",
		MaxSize: 1000,
		TTL:     5 * time.Minute,
	})

	// 创建聚合
	user := &UserAggregate{
		ID:      123,
		Name:    "Alice",
		Version: 1,
	}

	// 缓存聚合
	cache.Set(user.ID, user)

	// 从缓存加载
	cached, found := cache.Get(123)
	require.True(t, found)
	assert.Equal(t, "Alice", cached.Name)
	assert.Equal(t, 1, cached.Version)

	// 更新聚合
	user.Version = 2
	cache.Set(user.ID, user)

	// 验证更新
	cached, found = cache.Get(123)
	require.True(t, found)
	assert.Equal(t, 2, cached.Version)
}

// BenchmarkCache_Set 基准测试 - Set 操作
func BenchmarkCache_Set(b *testing.B) {
	cache := New[int, int](Config{
		Name:    "bench",
		MaxSize: 10000,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(i%10000, i)
	}
}

// BenchmarkCache_Get 基准测试 - Get 操作
func BenchmarkCache_Get(b *testing.B) {
	cache := New[int, int](Config{
		Name:    "bench",
		MaxSize: 10000,
	})

	// 预填充
	for i := 0; i < 10000; i++ {
		cache.Set(i, i*2)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(i % 10000)
	}
}

// BenchmarkCache_GetParallel 基准测试 - 并发 Get
func BenchmarkCache_GetParallel(b *testing.B) {
	cache := New[int, int](Config{
		Name:    "bench",
		MaxSize: 10000,
	})

	// 预填充
	for i := 0; i < 10000; i++ {
		cache.Set(i, i*2)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Get(i % 10000)
			i++
		}
	})
}
