package cache

import (
	"strconv"
	"testing"
	"time"

	"gochen/clock"
	"gochen/testkit/assert"
	"gochen/testkit/require"
)

// —— P2#19（fingerprint 7a1eb1b8）修复验证：Set 机会式清扫按扫描数截断 ——

// TestSet_OpportunisticSweep_BoundedByScanCount 确定性断言：无上限（MaxSize=0）
// + TTL 的大缓存且全部条目未过期时，单次 Set 的机会式清扫扫描条目数不超过
// maxOpportunisticSweep。修复前截断条件按驱逐数判断，无过期项时 evicted 恒 0，
// 扫描不设防——每次 Set 持写锁全表扫描（实测 8k 条 ≈1.2s/次，O(n²) 累计）。
func TestSet_OpportunisticSweep_BoundedByScanCount(t *testing.T) {
	c := New[string, int](Config{
		Name:    "sweep-scan-bound",
		MaxSize: 0,
		TTL:     time.Hour, // 条目远未过期，驱逐数恒 0
	})

	const total = 8000
	for i := 0; i < total; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	// 再写入一个新 key：触发机会式清扫路径。
	c.Set("probe", 0)

	// 缓存规模（8001）远大于扫描上限：扫描量恰为上限（确定性，不赌遍历顺序）。
	scanned := c.lastOpportunisticSweepScans()
	assert.Equal(t, maxOpportunisticSweep, scanned)
	// 无过期项：不应产生任何驱逐。
	assert.Equal(t, int64(0), c.Stats().Expires)
	assert.Equal(t, total+1, c.Size())
}

// TestSet_OpportunisticSweep_EvictionCappedByScanBudget 验证驱逐数作为次要
// 约束被扫描数上限收窄：全部条目已过期时，单次 Set 最多驱逐 maxOpportunisticSweep
// 个条目（扫描满即停），剩余过期条目由后续 Set 兜底逐批清空。
//
// 断言均不依赖 map 遍历顺序：第一次 Set 扫描量恰为上限（缓存规模 > 上限时
// 确定），驱逐数只断言上界；随后持续 Set 驱逐，只要剩余条目数 ≤ 上限即全量
// 扫描，故有限次内全部过期条目必被清空（总驱逐数恰好等于过期条目总数）。
func TestSet_OpportunisticSweep_EvictionCappedByScanBudget(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	c := New[string, int](Config{
		Name:    "sweep-eviction-cap",
		MaxSize: 0,
		TTL:     time.Hour,
		Clock:   clk,
	})

	const total = 40 // 2.5 倍于扫描上限，验证截断不是恰好全清
	for i := 0; i < total; i++ {
		c.Set(strconv.Itoa(i), i)
	}
	// 全部条目过期。
	clk.Advance(2 * time.Hour)

	// 第一次 Set：扫描量恰为上限；驱逐数被扫描预算收窄（≤ 上限），不是全清。
	c.Set("probe-1", 0)
	assert.Equal(t, maxOpportunisticSweep, c.lastOpportunisticSweepScans())
	expires := c.Stats().Expires
	require.Truef(t, expires > 0 && expires <= maxOpportunisticSweep,
		"first sweep should evict between 1 and %d entries, got %d", maxOpportunisticSweep, expires)
	require.True(t, c.Size() > 1, "single sweep must not have drained the cache")

	// 持续 Set 驱逐：有限次内清空全部过期条目（每次 Set 兜底清扫一批）。
	const drainSets = 6
	for i := 2; i <= drainSets; i++ {
		c.Set("drain-"+strconv.Itoa(i), 0)
	}
	assert.Equal(t, int64(total), c.Stats().Expires)
	assert.Equal(t, drainSets, c.Size())
}

// TestSet_OpportunisticSweep_PerfSmoke 性能冒烟（宽容阈值防 flake）：8k 条
// 无过期项插入的总耗时应远低于修复前的 O(n²) 量级（实测修复前 8k 全量扫
// 累计 ≈1.2s+）；修复后每次 Set 持锁扫描量 ≤ maxOpportunisticSweep，整体
// 应在几十毫秒级。阈值放宽到 500ms。
func TestSet_OpportunisticSweep_PerfSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf smoke in short mode")
	}
	c := New[string, int](Config{
		Name:    "sweep-perf-smoke",
		MaxSize: 0,
		TTL:     time.Hour,
	})

	start := time.Now()
	const total = 8000
	for i := 0; i < total; i++ {
		c.Set(strconv.Itoa(i), i)
	}
	elapsed := time.Since(start)

	require.Truef(t, elapsed < 500*time.Millisecond,
		"inserting %d entries took %v, expected well below the pre-fix O(n²) magnitude", total, elapsed)
}
