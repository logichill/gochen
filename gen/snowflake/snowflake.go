// Package snowflake 提供分布式ID生成器（雪花算法）
package snowflake

import (
	"reflect"
	"sync"
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
)

// Clock 定义雪花算法所需的最小时间源。
// 该接口与 clock.IClock 结构兼容：任何实现 Now() time.Time 的时钟（如 clock.RealClock、clock.ManualClock）均可直接注入。
type Clock interface {
	Now() time.Time
}

// wallClock 是缺省的系统墙钟实现。
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

// Option 配置 Generator 的可选参数。
type Option func(*Generator)

// WithClock 注入自定义时间源；缺省使用系统墙钟。
// 测试中注入可控时钟可获得确定性的 ID 序列；注入的时钟必须单调前进，回拨将返回错误。
// 同一毫秒序列耗尽时，自定义时钟必须先推进后重试，生成器不会在不可控的时钟上永久忙等。
func WithClock(c Clock) Option {
	return func(g *Generator) {
		if !isNilClock(c) {
			g.timeSrc = c
			g.customClock = true
		}
	}
}

// Generator 定义Generator。
type Generator struct {
	mux           sync.Mutex
	datacenterID  int64
	workerID      int64
	sequence      int64
	lastTimestamp int64
	timeSrc       Clock
	customClock   bool
}

// NewGenerator 创建Generator。
func NewGenerator(datacenterID, workerID int64, opts ...Option) (*Generator, error) {
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

	g := &Generator{
		datacenterID:  datacenterID,
		workerID:      workerID,
		sequence:      0,
		lastTimestamp: -1,
		timeSrc:       wallClock{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g, nil
}

func isNilClock(c Clock) bool {
	if c == nil {
		return true
	}
	rv := reflect.ValueOf(c)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// NextID 生成下一个ID。
func (g *Generator) NextID() (int64, error) {
	if g == nil {
		return 0, errors.NewCode(errors.InvalidInput, "snowflake generator is nil")
	}
	g.mux.Lock()
	defer g.mux.Unlock()

	timeSrc := g.timeSrc
	if isNilClock(timeSrc) {
		timeSrc = wallClock{}
	}
	now := timeSrc.Now().UnixMilli()
	if now < epoch {
		return 0, errors.NewCode(errors.FailedPrecondition, "clock is before snowflake epoch").
			WithContext("now_ms", now).
			WithContext("epoch_ms", epoch)
	}

	if now < g.lastTimestamp {
		return 0, errors.NewCode(errors.Internal, "clock moved backwards, refusing to generate id").
			WithContext("now_ms", now).
			WithContext("last_timestamp_ms", g.lastTimestamp)
	}

	if now == g.lastTimestamp {
		if g.sequence == maxSequence {
			next := timeSrc.Now().UnixMilli()
			if next < g.lastTimestamp {
				return 0, errors.NewCode(errors.Internal, "clock moved backwards, refusing to generate id").
					WithContext("now_ms", next).
					WithContext("last_timestamp_ms", g.lastTimestamp)
			}
			if g.customClock && next == g.lastTimestamp {
				return 0, errors.NewCode(errors.FailedPrecondition, "snowflake sequence exhausted before custom clock advanced").
					WithContext("timestamp_ms", g.lastTimestamp).
					WithContext("max_sequence", maxSequence)
			}
			for next <= g.lastTimestamp {
				next = timeSrc.Now().UnixMilli()
			}
			now = next
			g.sequence = 0
		} else {
			g.sequence++
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

// Next 实现 gen.IGenerator[int64] 约定。
func (g *Generator) Next() (int64, error) {
	if g == nil {
		return 0, errors.NewCode(errors.InvalidInput, "snowflake generator is nil")
	}
	return g.NextID()
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
