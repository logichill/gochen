package cache

import (
	"sync/atomic"
	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"
)

const cacheConcurrencyTestTimeout = 2 * time.Second

// TestCache_ConcurrentAccess 验证 Cache ConcurrentAccess。
func TestCache_ConcurrentAccess(t *testing.T) {
	cache := New[int, int](Config{
		Name:    "test",
		MaxSize: 1000,
	})

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

	for g := 0; g < goroutines; g++ {
		<-done
	}

	for g := 0; g < goroutines; g++ {
		for i := 0; i < iterations; i++ {
			key := g*iterations + i
			value, found := cache.Get(key)
			assert.True(t, found)
			assert.Equal(t, key*2, value)
		}
	}
}

// TestCache_ConcurrentReadWriteAndExpiry 验证 Cache ConcurrentReadWriteAndExpiry。
func TestCache_ConcurrentReadWriteAndExpiry(t *testing.T) {
	cache := New[int, int](Config{
		Name:    "concurrent_expiry",
		MaxSize: 1024,
		TTL:     50 * time.Millisecond,
	})

	const (
		goroutines = 8
		iterations = 500
	)

	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				_ = cache.CleanExpired()
				_ = cache.Stats()
			}
		}
	}()

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			for i := 0; i < iterations; i++ {
				key := i % 256
				cache.Set(key, id+i)
				_, _ = cache.Get(key)
			}
			done <- struct{}{}
		}(g)
	}

	for g := 0; g < goroutines; g++ {
		<-done
	}
	close(stopCh)

	size := cache.Size()
	if size < 0 || size > 1024 {
		t.Fatalf("unexpected cache size after concurrent access: %d", size)
	}
}

// TestCache_AggregateUseCase 验证 Cache AggregateUseCase。
func TestCache_AggregateUseCase(t *testing.T) {
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

	user := &UserAggregate{
		ID:      123,
		Name:    "Alice",
		Version: 1,
	}

	cache.Set(user.ID, user)

	cached, found := cache.Get(123)
	require.True(t, found)
	assert.Equal(t, "Alice", cached.Name)
	assert.Equal(t, 1, cached.Version)

	user.Version = 2
	cache.Set(user.ID, user)

	cached, found = cache.Get(123)
	require.True(t, found)
	assert.Equal(t, 2, cached.Version)
}

func TestCache_OnEvictRunsOutsideLock(t *testing.T) {
	var callbackCount int32
	var cache *Cache[int, string]
	cache = New[int, string](Config{
		Name:    "reentrant_evict",
		MaxSize: 1,
		OnEvict: func(key, value any) {
			atomic.AddInt32(&callbackCount, 1)
			_, _ = key.(int)
			_, _ = value.(string)
			_, _ = cache.Get(2)
			_ = cache.Size()
		},
	})

	done := make(chan struct{})
	go func() {
		cache.Set(1, "one")
		cache.Set(2, "two")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(cacheConcurrencyTestTimeout):
		t.Fatalf("expected reentrant OnEvict callback not to deadlock")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&callbackCount))
}

func TestCache_ClearDoesNotDropConcurrentSet(t *testing.T) {
	onEvictStarted := make(chan struct{})
	unblockEvict := make(chan struct{})

	cache := New[int, string](Config{
		Name: "clear_race",
		OnEvict: func(key, value any) {
			close(onEvictStarted)
			<-unblockEvict
		},
	})

	cache.Set(1, "one")

	clearDone := make(chan struct{})
	go func() {
		cache.Clear()
		close(clearDone)
	}()

	select {
	case <-onEvictStarted:
	case <-time.After(cacheConcurrencyTestTimeout):
		t.Fatalf("expected OnEvict to be called")
	}

	cache.Set(2, "two")
	close(unblockEvict)

	select {
	case <-clearDone:
	case <-time.After(cacheConcurrencyTestTimeout):
		t.Fatalf("expected Clear to finish")
	}

	value, found := cache.Get(2)
	require.True(t, found)
	assert.Equal(t, "two", value)
}
