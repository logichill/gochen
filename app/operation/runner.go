package operation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gochen/app/operation/internal/singleflight"
	"gochen/gen/uuid"
)

// Handler 表示真正执行业务写逻辑的回调。
type Handler func(ctx context.Context) (*Result, error)

// IRunner 统一包装写操作的协议输出。
type IRunner interface {
	Execute(ctx context.Context, spec *Spec, fn Handler) (*Result, error)
}

// RunnerOptions 控制默认 runner 的轻量行为。
type RunnerOptions struct {
	IDGenerator         func() string
	StatusURLBuilder    func(operationID string) string
	StreamURLBuilder    func(operationID string) string
	DefaultRetryAfterMs int
	Store               IStore
}

// Runner 执行 operation 协议包装、幂等协调与可选状态持久化。
type Runner struct {
	idGenerator         func() string
	statusURLBuilder    func(operationID string) string
	streamURLBuilder    func(operationID string) string
	defaultRetryAfterMs int
	store               IStore
	idempotencyMu       sync.Mutex
	idempotency         singleflight.Registry[string, *Result]
}

var (
	defaultRunner        = NewRunner(nil)
	operationFallbackSeq uint64
)

var _ IRunner = (*Runner)(nil)

// DefaultRunner 返回框架默认的轻量 runner。
func DefaultRunner() *Runner {
	return defaultRunner
}

// NewRunner 创建最小可用的 operation runner。
func NewRunner(opts *RunnerOptions) *Runner {
	r := &Runner{
		idGenerator: defaultOperationID,
		idempotency: singleflight.NewRegistry[string, *Result](),
	}
	if opts != nil {
		if opts.IDGenerator != nil {
			r.idGenerator = opts.IDGenerator
		}
		r.statusURLBuilder = opts.StatusURLBuilder
		r.streamURLBuilder = opts.StreamURLBuilder
		r.defaultRetryAfterMs = opts.DefaultRetryAfterMs
		r.store = opts.Store
	}
	return r
}

func defaultOperationID() string {
	id, err := uuid.NewV7()
	if err == nil && strings.TrimSpace(id) != "" {
		return "op_" + id
	}
	return fallbackOperationID()
}

func fallbackOperationID() string {
	seq := atomic.AddUint64(&operationFallbackSeq, 1)
	return fmt.Sprintf("op_%d_%d", time.Now().UnixNano(), seq)
}
