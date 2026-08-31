package testkit

import (
	"sync"

	"gochen/errors"
)

// GeneratorFunc 把函数适配为 gen.IGenerator。
type GeneratorFunc[ID comparable] func() (ID, error)

// Next 调用被包装的生成函数。
func (f GeneratorFunc[ID]) Next() (ID, error) {
	if f == nil {
		var zero ID
		return zero, errors.NewCode(errors.InvalidInput, "generator function is nil")
	}
	return f()
}

// Int64Sequence 是并发安全、从指定值开始的 int64 序列生成器。
type Int64Sequence struct {
	mu   sync.Mutex
	next int64
}

// NewInt64Sequence 创建首个返回值为 first 的序列生成器。
func NewInt64Sequence(first int64) *Int64Sequence {
	return &Int64Sequence{next: first}
}

// Next 返回当前序列值并推进到下一项。
func (g *Int64Sequence) Next() (int64, error) {
	if g == nil {
		return 0, errors.NewCode(errors.InvalidInput, "sequence generator is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.next <= 0 {
		return 0, errors.NewCode(errors.FailedPrecondition, "int64 sequence is exhausted or invalid").
			WithContext("next", g.next)
	}
	id := g.next
	if id == int64(^uint64(0)>>1) {
		g.next = 0
	} else {
		g.next++
	}
	return id, nil
}
