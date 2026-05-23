package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCache_BasicOperations 验证 Cache BasicOperations。
func TestCache_BasicOperations(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     time.Minute,
	})

	cache.Set("key1", 100)
	value, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, 100, value)

	_, found = cache.Get("nonexistent")
	assert.False(t, found)

	deleted := cache.Delete("key1")
	assert.True(t, deleted)

	_, found = cache.Get("key1")
	assert.False(t, found)

	deleted = cache.Delete("key1")
	assert.False(t, deleted)
}

// TestCache_Update 验证 Cache Update。
func TestCache_Update(t *testing.T) {
	cache := New[int64, string](Config{
		Name:    "test",
		MaxSize: 100,
	})

	cache.Set(1, "first")
	value, found := cache.Get(1)
	require.True(t, found)
	assert.Equal(t, "first", value)

	cache.Set(1, "second")
	value, found = cache.Get(1)
	require.True(t, found)
	assert.Equal(t, "second", value)

	assert.Equal(t, 1, cache.Size())
}

// TestCache_LRUEviction 验证 Cache LRUEviction。
func TestCache_LRUEviction(t *testing.T) {
	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 3,
	})

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	assert.Equal(t, 3, cache.Size())

	_, found := cache.Get(1)
	assert.True(t, found)

	cache.Set(4, "four")

	assert.Equal(t, 3, cache.Size())

	_, found = cache.Get(2)
	assert.False(t, found)

	_, found = cache.Get(1)
	assert.True(t, found)
	_, found = cache.Get(3)
	assert.True(t, found)
	_, found = cache.Get(4)
	assert.True(t, found)

	stats := cache.Stats()
	assert.Equal(t, int64(1), stats.Evictions)
}

// TestCache_Clear 验证 Cache Clear。
func TestCache_Clear(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
	})

	for i := 0; i < 10; i++ {
		cache.Set(string(rune('a'+i)), i)
	}

	assert.Equal(t, 10, cache.Size())

	cache.Clear()

	assert.Equal(t, 0, cache.Size())

	for i := 0; i < 10; i++ {
		_, found := cache.Get(string(rune('a' + i)))
		assert.False(t, found)
	}
}

// TestCache_OnEvict 验证 Cache OnEvict。
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
	cache.Set(3, "three")

	assert.Equal(t, 1, len(evicted))
	assert.Equal(t, "one", evicted[1])

	cache.Delete(2)

	assert.Equal(t, 2, len(evicted))
	assert.Equal(t, "two", evicted[2])

	evicted = make(map[int]string)
	cache.Clear()
	assert.Equal(t, 1, len(evicted))
	assert.Equal(t, "three", evicted[3])
}
