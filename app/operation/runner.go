package operation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gochen/app/internal/depcheck"
	"gochen/app/operation/internal/singleflight"
	"gochen/contextx"
	"gochen/errors"
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

type contextKey string

const operationIDContextKey contextKey = "gochen/app/operation/id"

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

// WithOperationID 将 operation id 注入上下文，供业务侧读取。
func WithOperationID(ctx context.Context, operationID string) context.Context {
	if ctx == nil || operationID == "" {
		return ctx
	}
	return context.WithValue(ctx, operationIDContextKey, operationID)
}

// OperationIDFromContext 读取当前上下文中的 operation id。
func OperationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(operationIDContextKey).(string)
	return value
}

// Execute 统一包装写入口的 operation envelope。
func (r *Runner) Execute(ctx context.Context, spec *Spec, fn Handler) (*Result, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.NewCode(errors.InvalidInput, "operation runner is nil")
	}
	if r.store != nil && depcheck.IsNil(r.store) {
		return nil, errors.NewCode(errors.InvalidInput, "operation store contains a nil value")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, errors.NewCode(errors.InvalidInput, "operation handler cannot be nil")
	}

	idempotencyKey := strings.TrimSpace(spec.IdempotencyKey)
	idempotencyStore, hasIdempotency := r.store.(IIdempotencyStore)
	if result, handled, err := r.executeIdempotency(ctx, spec, fn, idempotencyKey, idempotencyStore, hasIdempotency); handled {
		return result, err
	}

	return r.execute(ctx, spec, fn, executeConfig{
		idempotencyKey:   idempotencyKey,
		idempotencyStore: idempotencyStore,
		hasIdempotency:   hasIdempotency,
	})
}

func (r *Runner) execute(ctx context.Context, spec *Spec, fn Handler, cfg executeConfig) (*Result, error) {
	if spec.Mode == ModeTracked && cfg.idempotencyKey != "" && cfg.hasIdempotency {
		// 先读已完成结果，命中时直接复用，未命中再执行业务回调。
		if existing, err := cfg.idempotencyStore.GetByIdempotencyKey(ctx, cfg.idempotencyKey); err == nil {
			return existing, nil
		} else if !IsStoreNotFound(err) {
			return nil, err
		}
	}

	operationID := ""
	execCtx := ctx
	if spec.Mode == ModeTracked {
		operationID = strings.TrimSpace(r.idGenerator())
		if operationID == "" {
			return nil, errors.NewCode(errors.Internal, "tracked operation id generator returned empty id")
		}
		if result, reserved, err := r.reserveIdempotencyKey(ctx, operationID, cfg); err != nil || reserved {
			return result, err
		}
		defer r.releaseIdempotencyReservationOnPanic(ctx, operationID, cfg)
		execCtx = WithOperationID(execCtx, operationID)
	}

	result, err := fn(execCtx)
	if err != nil {
		failed := r.failedResult(spec, operationID, result, err)
		if spec.Mode != ModeTracked {
			return failed, err
		}
		if stored, replayed, storeErr := r.storeResult(execCtx, failed, cfg); storeErr != nil {
			if !contextx.IsAfterCommitError(err) {
				_ = r.releaseIdempotencyReservation(ctx, operationID, cfg)
			}
			return failed, errors.Join(err, storeErr)
		} else if replayed {
			return stored, nil
		} else {
			return stored, err
		}
	}
	status := successStatusForResult(result, spec.Mode)
	merged := r.buildMergedResult(spec, operationID, result, status, nil)
	if spec.Mode == ModeTracked {
		stored, _, err := r.storeResult(execCtx, merged, cfg)
		if err != nil {
			// 业务回调已成功，不能释放预占后让重试再次执行业务。
			// 调用方使用返回的 operation ID 补存结果或人工核对。
			return merged, contextx.WrapAfterCommitError(err)
		}
		return stored, nil
	}
	return merged, nil
}

func (r *Runner) storeResult(ctx context.Context, result *Result, cfg executeConfig) (*Result, bool, error) {
	if r.store == nil {
		return result, false, nil
	}
	if cfg.idempotencyKey != "" && cfg.hasIdempotency {
		if err := cfg.idempotencyStore.PutWithIdempotencyKey(ctx, cfg.idempotencyKey, result); err != nil {
			if errors.Is(err, errors.Conflict) {
				existing, getErr := cfg.idempotencyStore.GetByIdempotencyKey(ctx, cfg.idempotencyKey)
				if getErr == nil {
					return existing, true, nil
				}
				if !IsStoreNotFound(getErr) {
					return nil, false, getErr
				}
			}
			return nil, false, err
		}
	} else {
		if err := r.store.Put(ctx, result); err != nil {
			return nil, false, err
		}
	}
	return result, false, nil
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
