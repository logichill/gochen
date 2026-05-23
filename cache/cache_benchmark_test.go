package cache

import "testing"

// BenchmarkCache_Set 用于评估 Cache Set 的性能。
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

// BenchmarkCache_Get 用于评估 Cache Get 的性能。
func BenchmarkCache_Get(b *testing.B) {
	cache := New[int, int](Config{
		Name:    "bench",
		MaxSize: 10000,
	})

	for i := 0; i < 10000; i++ {
		cache.Set(i, i*2)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(i % 10000)
	}
}

// BenchmarkCache_GetParallel 用于评估 Cache GetParallel 的性能。
func BenchmarkCache_GetParallel(b *testing.B) {
	cache := New[int, int](Config{
		Name:    "bench",
		MaxSize: 10000,
	})

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
