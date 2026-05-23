// Package snowflake 提供分布式ID生成器（雪花算法）
package snowflake

import (
	"sync"
	"sync/atomic"
	"time"

	"gochen/errors"
)

const (
	// 起始时间戳 (2023-01-01 00:00:00 UTC)
	epoch int64 = 1672531200000

	// 各部分位数
	workerIDBits     = 5
	datacenterIDBits = 5
	sequenceBits     = 12

	// 最大值
	maxWorkerID     = -1 ^ (-1 << workerIDBits)     // 31
	maxDatacenterID = -1 ^ (-1 << datacenterIDBits) // 31
	maxSequence     = -1 ^ (-1 << sequenceBits)     // 4095

	// 位移
	workerIDShift      = sequenceBits
	datacenterIDShift  = sequenceBits + workerIDBits
	timestampLeftShift = sequenceBits + workerIDBits + datacenterIDBits

	// 默认配置
	DefaultDatacenterID int64 = 1
	// DefaultWorkerID 是常量。
	DefaultWorkerID int64 = 1
)

// Generator 定义Generator。
type Generator struct {
	mux           sync.Mutex
	datacenterID  int64
	workerID      int64
	sequence      int64
	lastTimestamp int64
}

// NewGenerator 创建Generator。
func NewGenerator(datacenterID, workerID int64) (*Generator, error) {
	if datacenterID < 0 || datacenterID > maxDatacenterID {
		return nil, errors.NewCode(errors.InvalidInput, "datacenter ID out of range").
			WithContext("datacenter_id", datacenterID).
			WithContext("max_datacenter_id", maxDatacenterID)
	}

	if workerID < 0 || workerID > maxWorkerID {
		return nil, errors.NewCode(errors.InvalidInput, "worker ID out of range").
			WithContext("worker_id", workerID).
			WithContext("max_worker_id", maxWorkerID)
	}

	return &Generator{
		datacenterID:  datacenterID,
		workerID:      workerID,
		sequence:      0,
		lastTimestamp: -1,
	}, nil
}

// NextID 生成下一个ID。
func (g *Generator) NextID() (int64, error) {
	g.mux.Lock()
	defer g.mux.Unlock()

	now := time.Now().UnixNano() / 1e6

	if now < g.lastTimestamp {
		return 0, errors.NewCode(errors.Internal, "clock moved backwards, refusing to generate id").
			WithContext("now_ms", now).
			WithContext("last_timestamp_ms", g.lastTimestamp)
	}

	if now == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// 序列号用完，等待下一毫秒
			for now <= g.lastTimestamp {
				now = time.Now().UnixNano() / 1e6
			}
		}
	} else {
		g.sequence = 0
	}

	g.lastTimestamp = now

	id := ((now - epoch) << timestampLeftShift) |
		(g.datacenterID << datacenterIDShift) |
		(g.workerID << workerIDShift) |
		g.sequence

	return id, nil
}

// Parse 解析输入。
func Parse(id int64) map[string]int64 {
	return map[string]int64{
		"timestamp":    (id >> timestampLeftShift) + epoch,
		"datacenterID": (id >> datacenterIDShift) & maxDatacenterID,
		"workerID":     (id >> workerIDShift) & maxWorkerID,
		"sequence":     id & maxSequence,
	}
}

// 全局默认生成器（通过原子指针保证并发安全）
var defaultGenerator atomic.Pointer[Generator]

// init 执行包初始化逻辑。
func init() {
	gen, err := NewGenerator(DefaultDatacenterID, DefaultWorkerID)
	if err == nil {
		defaultGenerator.Store(gen)
	}
}

// NextID 使用默认生成器生成ID。
func NextID() (int64, error) {
	gen := defaultGenerator.Load()
	if gen == nil {
		return 0, errors.NewCode(errors.Internal, "default generator is not initialized")
	}
	return gen.NextID()
}

// SetDefaultGenerator 设置默认生成器。
func SetDefaultGenerator(datacenterID, workerID int64) error {
	gen, err := NewGenerator(datacenterID, workerID)
	if err != nil {
		return err
	}
	defaultGenerator.Store(gen)
	return nil
}

// InitDefault 注册数据。
//
// 说明：
// - InitDefault 初始化默认生成器。
func InitDefault() {
	_ = SetDefaultGenerator(DefaultDatacenterID, DefaultWorkerID)
}

// InitGenerator 注册数据。
//
// 说明：
// - InitGenerator 使用自定义配置初始化默认生成器。
func InitGenerator(datacenterID, workerID int64) error {
	return SetDefaultGenerator(datacenterID, workerID)
}
