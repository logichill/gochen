package operation

import (
	"context"
	"fmt"
	"time"

	"gochen/app/operation/internal/singleflight"
	"gochen/contextx"
	"gochen/errors"
)

const idempotencyReservationCleanupTimeout = 5 * time.Second

// idempotencyReservationRetryBackoff 预占冲突后两轮重试之间的退避：
// 无退避的两轮重试在亚毫秒内耗尽，新 Leader 报告旧操作仍在异步清理时
// 极易误判为预留过期直接抛错。
const idempotencyReservationRetryBackoff = 25 * time.Millisecond

func (r *Runner) executeIdempotency(
	ctx context.Context,
	spec *Spec,
	fn Handler,
	key string,
	store IIdempotencyStore,
	hasStore bool,
) (*Result, bool, error) {
	if spec.Mode != ModeTracked || key == "" {
		return nil, false, nil
	}
	if !hasStore {
		return nil, true, errors.NewCode(errors.InvalidInput, "tracked operation idempotency key requires idempotency store")
	}
	result, err := r.doWithIdempotencyKey(ctx, key, store, func() (*Result, error) {
		return r.execute(ctx, spec, fn, executeConfig{
			idempotencyKey:   key,
			idempotencyStore: store,
			hasIdempotency:   hasStore,
		})
	})
	return result, true, err
}

func (r *Runner) doWithIdempotencyKey(ctx context.Context, key string, store IIdempotencyStore, fn func() (*Result, error)) (*Result, error) {
	// Runner 统一负责进程内单飞；幂等结果读写由 store 负责。
	for {
		call, owner, err := r.beginIdempotencyCall(ctx, key)
		if err != nil {
			return nil, err
		}
		result, err := singleflight.Run(ctx, call, owner,
			waitRunnerIdempotencyCall,
			func(call *singleflight.Call[*Result], result *Result, err error) {
				r.finishIdempotencyCall(key, call, result, err)
			},
			func(recovered any) error { return idempotencyPanicError(recovered) },
			fn,
		)
		if !owner &&
			!contextx.IsAfterCommitError(err) &&
			(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) &&
			ctx.Err() == nil {
			// Leader 可能已把终态失败落库（如业务回调下游超时，StatusFailed 已持久化后返回）。
			// 先查已落库结果：若已是失败终态则直接透传存储的错误，不重试；
			// 否则重试会让 execute 命中已落库记录并把错误伪装成成功。
			if existing, getErr := retryableIdempotencyResult(ctx, store, key); getErr == nil && existing != nil && storedResultIsFailure(existing) {
				return existing, storedResultError(existing)
			}
			continue
		}
		return result, err
	}
}

// retryableIdempotencyResult 读取已落库的幂等结果（仅用于 Follower 重试裁决）。
func retryableIdempotencyResult(ctx context.Context, store IIdempotencyStore, key string) (*Result, error) {
	if store == nil {
		return nil, errors.NewCode(errors.InvalidInput, "idempotency store is nil")
	}
	return store.GetByIdempotencyKey(ctx, key)
}

// storedResultIsFailure 判断已落库结果是否为失败终态且携带结构化错误。
func storedResultIsFailure(result *Result) bool {
	return result != nil &&
		(result.Operation.Status == StatusFailed || result.Operation.Status == StatusTimeout) &&
		result.Error != nil
}

// storedResultError 从已落库的失败结果重建错误对象，保持错误码与错误信息可见。
func storedResultError(result *Result) error {
	if result == nil || result.Error == nil {
		return errors.NewCode(errors.Internal, "operation stored failure is not reconstructable")
	}
	code := errors.ErrorCode(result.Error.Code)
	if code == "" {
		code = errors.Internal
	}
	err := errors.NewCode(code, result.Error.Message)
	if len(result.Error.Details) > 0 {
		err = err.WithDetails(result.Error.Details)
	}
	return err
}

func (r *Runner) beginIdempotencyCall(ctx context.Context, key string) (*singleflight.Call[*Result], bool, error) {
	r.idempotencyMu.Lock()
	defer r.idempotencyMu.Unlock()
	return r.idempotency.Begin(ctx, key)
}

func waitRunnerIdempotencyCall(ctx context.Context, call *singleflight.Call[*Result]) (*Result, error) {
	return call.Wait(ctx, singleflight.WaitOptions[*Result]{
		Clone:     CloneResult,
		HasResult: hasOperationResult,
	})
}

func (r *Runner) finishIdempotencyCall(key string, call *singleflight.Call[*Result], result *Result, err error) {
	r.idempotencyMu.Lock()
	defer r.idempotencyMu.Unlock()
	r.idempotency.Finish(key, call, result, err, singleflight.FinishOptions[*Result]{
		Clone:       CloneResult,
		KeepResult:  hasOperationResult,
		KeepOnError: true,
	})
}

func idempotencyPanicError(recovered any) errors.IAppError {
	panicValue := fmt.Sprint(recovered)
	return errors.NewCode(errors.Internal, "idempotent operation panicked: "+panicValue).
		WithContext("panic", panicValue)
}

func hasOperationResult(result *Result) bool {
	return result != nil && result.Operation.ID != ""
}

type executeConfig struct {
	idempotencyKey   string
	idempotencyStore IIdempotencyStore
	hasIdempotency   bool
}

func (r *Runner) reserveIdempotencyKey(ctx context.Context, operationID string, cfg executeConfig) (*Result, bool, error) {
	if cfg.idempotencyKey == "" || !cfg.hasIdempotency {
		return nil, false, nil
	}
	reserver, ok := cfg.idempotencyStore.(IIdempotencyReservationStore)
	if !ok {
		return nil, false, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		err := reserver.ReserveIdempotencyKey(ctx, cfg.idempotencyKey, operationID)
		if err == nil {
			return nil, false, nil
		}
		if errors.Is(err, errors.Conflict) {
			existing, getErr := cfg.idempotencyStore.GetByIdempotencyKey(ctx, cfg.idempotencyKey)
			if getErr == nil {
				return existing, true, nil
			}
			if !IsStoreNotFound(getErr) {
				return nil, true, getErr
			}
			// 旧预留正在异步清理：带微退避重试，避免亚毫秒内误判 stale。
			if attempt == 0 {
				time.Sleep(idempotencyReservationRetryBackoff)
			}
			continue
		}
		return nil, true, err
	}
	return nil, true, errors.NewCode(errors.Conflict, "idempotency reservation is stale").
		WithContext("idempotency_key", cfg.idempotencyKey)
}

func (r *Runner) releaseIdempotencyReservationOnPanic(ctx context.Context, operationID string, cfg executeConfig) {
	if recovered := recover(); recovered != nil {
		func() {
			defer func() {
				_ = recover()
			}()
			_ = r.releaseIdempotencyReservation(ctx, operationID, cfg)
		}()
		panic(recovered)
	}
}

func (r *Runner) releaseIdempotencyReservation(ctx context.Context, operationID string, cfg executeConfig) error {
	if cfg.idempotencyKey == "" || operationID == "" || !cfg.hasIdempotency {
		return nil
	}
	releaser, ok := cfg.idempotencyStore.(IIdempotencyReservationReleaseStore)
	if !ok {
		return nil
	}
	// 结果保存失败时请求可能已经取消，预留清理仍需独立完成并保留上下文路由信息。
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idempotencyReservationCleanupTimeout)
	defer cancel()
	return releaser.ReleaseIdempotencyKey(cleanupCtx, cfg.idempotencyKey, operationID)
}
