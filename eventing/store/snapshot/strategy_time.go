package snapshot

import (
	"context"
	"sync"
	"time"
)

// DefaultStrategyCacheMaxKeys 策略侧内存缓存的默认最大键数。
// 防止高基数聚合下 lastSnapshotTimes 无界增长造成内存泄漏。
const DefaultStrategyCacheMaxKeys = 10000

// TimeDurationStrategy 基于时间间隔的快照策略。
// 当距离上次快照超过指定时间时创建快照。
type TimeDurationStrategy[ID comparable] struct {
	Duration          time.Duration
	lastSnapshotTimes map[snapshotKey[ID]]time.Time // 聚合键 -> 上次快照时间
	snapshotStore     ISnapshotStore[ID]
	mutex             sync.RWMutex
	// MaxKeys 限制内存缓存的最大键数；<= 时使用默认值 DefaultStrategyCacheMaxKeys。
	// 超限时淘汰缓存时间最旧的条目（其缓存价值最低：已过期必然回源重查）。
	// 缓存只是优化：被淘汰的键下次直接回源 FindSnapshot 校准，不损失正确性。
	MaxKeys int
}

// NewTimeDurationStrategy 创建一个按时间间隔触发的快照策略。
func NewTimeDurationStrategy[ID comparable](duration time.Duration, snapshotStore ISnapshotStore[ID]) *TimeDurationStrategy[ID] {
	if duration <= 0 {
		duration = 24 * time.Hour // 默认24小时
	}
	return &TimeDurationStrategy[ID]{
		Duration:          duration,
		lastSnapshotTimes: make(map[snapshotKey[ID]]time.Time),
		snapshotStore:     snapshotStore,
		MaxKeys:           DefaultStrategyCacheMaxKeys,
	}
}

// ShouldCreateSnapshot 在距离上次快照超过阈值时返回 true。
func (s *TimeDurationStrategy[ID]) ShouldCreateSnapshot(ctx context.Context, aggregate ISnapshotAggregate[ID]) (bool, error) {
	aggregateID := aggregate.GetID()
	aggregateType := aggregate.GetAggregateType()
	key := newSnapshotKey(aggregateType, aggregateID)

	// 缓存命中新鲜快照时不访问持久化存储，避免每次判断都触发 DB 查询。
	s.mutex.RLock()
	lastTime, exists := s.lastSnapshotTimes[key]
	s.mutex.RUnlock()
	if exists && time.Since(lastTime) < s.Duration {
		return false, nil
	}

	// 缓存缺失或已过期时再查询持久化存储校准，避免旧缓存导致重复创建快照。
	if s.snapshotStore != nil {
		if snapshot, err := s.snapshotStore.FindSnapshot(ctx, aggregateType, aggregateID); err == nil {
			s.rememberLastSnapshotTime(key, snapshot.Timestamp)
			return time.Since(snapshot.Timestamp) >= s.Duration, nil
		}
	}

	return !exists || time.Since(lastTime) >= s.Duration, nil
}

func (s *TimeDurationStrategy[ID]) Name() string {
	return "TimeDurationStrategy"
}

// UpdateLastSnapshotTime 手动更新内存中的最近快照时间缓存。
func (s *TimeDurationStrategy[ID]) UpdateLastSnapshotTime(aggregateType string, aggregateID ID, timestamp time.Time) {
	s.mutex.Lock()
	key := newSnapshotKey(aggregateType, aggregateID)
	s.rememberLocked(key, timestamp)
	s.mutex.Unlock()
}

// rememberLastSnapshotTime 记录最近快照时间并维护容量上限（调用方无需持锁）。
func (s *TimeDurationStrategy[ID]) rememberLastSnapshotTime(key snapshotKey[ID], ts time.Time) {
	s.mutex.Lock()
	s.rememberLocked(key, ts)
	s.mutex.Unlock()
}

// rememberLocked 无锁版本的容量受控写缓存（调用方必须已持写锁）。
func (s *TimeDurationStrategy[ID]) rememberLocked(key snapshotKey[ID], ts time.Time) {
	maxKeys := s.MaxKeys
	if maxKeys <= 0 {
		maxKeys = DefaultStrategyCacheMaxKeys
	}
	if s.lastSnapshotTimes == nil {
		s.lastSnapshotTimes = make(map[snapshotKey[ID]]time.Time)
	}
	for len(s.lastSnapshotTimes) >= maxKeys {
		if !s.evictOldestLocked() {
			break
		}
	}
	s.lastSnapshotTimes[key] = ts
}

// evictOldestLocked 淘汰缓存时间最旧的条目；无法淘汰时返回 false。
func (s *TimeDurationStrategy[ID]) evictOldestLocked() bool {
	var oldestKey snapshotKey[ID]
	var oldestTime time.Time
	found := false
	for k, ts := range s.lastSnapshotTimes {
		if !found || ts.Before(oldestTime) {
			oldestKey, oldestTime, found = k, ts, true
		}
	}
	if !found {
		return false
	}
	delete(s.lastSnapshotTimes, oldestKey)
	return true
}
