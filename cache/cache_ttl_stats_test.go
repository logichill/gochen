package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCache_TTLExpiration 验证 Cache TTLExpiration。
func TestCache_TTLExpiration(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     100 * time.Millisecond,
	})

	cache.Set("key1", 100)

	value, found := cache.Get("key1")
	assert.True(t, found)
	assert.Equal(t, 100, value)

	time.Sleep(150 * time.Millisecond)

	_, found = cache.Get("key1")
	assert.False(t, found)

	stats := cache.Stats()
	assert.Equal(t, int64(1), stats.Expires)
}

// TestCache_TTLRefreshOnAccess 验证 Cache TTLRefreshOnAccess。
func TestCache_TTLRefreshOnAccess(t *testing.T) {
	cache := New[string, int](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     200 * time.Millisecond,
	})

	cache.Set("key1", 100)

	for i := 0; i < 3; i++ {
		time.Sleep(100 * time.Millisecond)
		value, found := cache.Get("key1")
		assert.True(t, found, "iteration %d", i)
		assert.Equal(t, 100, value)
	}

	_, found := cache.Get("key1")
	assert.True(t, found)
}

// TestCache_CleanExpired 验证 Cache CleanExpired。
func TestCache_CleanExpired(t *testing.T) {
	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 100,
		TTL:     100 * time.Millisecond,
	})

	cache.Set(1, "one")
	cache.Set(2, "two")
	cache.Set(3, "three")

	assert.Equal(t, 3, cache.Size())

	time.Sleep(150 * time.Millisecond)

	cleaned := cache.CleanExpired()
	assert.Equal(t, 3, cleaned)
	assert.Equal(t, 0, cache.Size())
}

// TestCache_Stats 验证 Cache Stats。
func TestCache_Stats(t *testing.T) {
	cache := New[int, string](Config{
		Name:    "test",
		MaxSize: 2,
		TTL:     100 * time.Millisecond,
	})

	stats := cache.Stats()
	assert.Equal(t, int64(0), stats.Hits)
	assert.Equal(t, int64(0), stats.Misses)
	assert.Equal(t, int64(0), stats.Evictions)

	cache.Set(1, "one")
	cache.Set(2, "two")

	_, found := cache.Get(1)
	assert.True(t, found)

	_, found = cache.Get(999)
	assert.False(t, found)

	cache.Set(3, "three")

	time.Sleep(150 * time.Millisecond)
	_, _ = cache.Get(1)

	stats = cache.Stats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(2), stats.Misses)
	assert.Equal(t, int64(1), stats.Evictions)
	assert.Equal(t, int64(1), stats.Expires)
}

// TestCache_HitRate 验证 Cache HitRate。
func TestCache_HitRate(t *testing.T) {
	cache := New[int, int](Config{
		Name:    "test",
		MaxSize: 100,
	})

	assert.Equal(t, 0.0, cache.HitRate())

	cache.Set(1, 100)

	for i := 0; i < 3; i++ {
		_, found := cache.Get(1)
		assert.True(t, found)
	}

	_, found := cache.Get(999)
	assert.False(t, found)

	assert.InDelta(t, 0.75, cache.HitRate(), 0.01)
}
