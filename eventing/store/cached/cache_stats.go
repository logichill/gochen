package cached

import (
	"sync"
)

// CacheStats 缓存统计信息。
type CacheStats struct {
	Hits          int64 // 缓存命中次数
	Misses        int64 // 缓存未命中次数
	Evictions     int64 // 缓存驱逐次数
	Invalidations int64 // 缓存失效次数
	mutex         sync.RWMutex
}
