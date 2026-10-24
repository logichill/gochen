package cached

import (
	"container/list"
	"sync"
	"time"

	"gochen/eventing"
	"gochen/messaging"
)

// EventCache 事件缓存。
type EventCache[ID comparable] struct {
	aggregateCache map[aggregateCacheKey[ID]]*CachedAggregate[ID] // 聚合缓存（key: aggregateType + aggregateID + tenantID）
	lru            *list.List                                     // aggregateCacheKey[ID], front is least recently used
	lruIndex       map[aggregateCacheKey[ID]]*list.Element
	generations    map[aggregateCacheKey[ID]]uint64
	activeFills    map[aggregateCacheKey[ID]]int
	ttl            time.Duration // 缓存过期时间
	maxAggregates  int           // 最大缓存聚合数
	mutex          sync.RWMutex
}

// CachedAggregate 缓存的聚合数据。
type CachedAggregate[ID comparable] struct {
	Events    []eventing.Event[ID] // 事件列表
	Version   uint64               // 当前版本
	CreatedAt time.Time            // 创建时间
}

type aggregateCacheKey[ID comparable] struct {
	aggregateType string
	aggregateID   ID
	tenantID      string
}

func cacheKey[ID comparable](aggregateType string, aggregateID ID) aggregateCacheKey[ID] {
	return aggregateCacheKey[ID]{aggregateType: aggregateType, aggregateID: aggregateID}
}

func cloneEvents[ID comparable](events []eventing.Event[ID]) []eventing.Event[ID] {
	if len(events) == 0 {
		return []eventing.Event[ID]{}
	}
	out := make([]eventing.Event[ID], len(events))
	for i := range events {
		out[i] = cloneEvent(events[i])
	}
	return out
}

func cloneEvent[ID comparable](evt eventing.Event[ID]) eventing.Event[ID] {
	clone := evt
	clone.Message = messaging.CloneMessage(evt.Message)
	return clone
}
