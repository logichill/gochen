package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
