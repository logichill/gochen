package operation

import (
	"context"
	"fmt"

	"time"

	"gochen/app/operation/internal/singleflight"

	"gochen/errors"
)

const idempotencyReservationCleanupTimeout = 5 * time.Second

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
	result, err := r.doWithIdempotencyKey(ctx, key, func() (*Result, error) {
		return r.execute(ctx, spec, fn, executeConfig{
			idempotencyKey:   key,
			idempotencyStore: store,
			hasIdempotency:   hasStore,
		})
	})
	return result, true, err
}

func (r *Runner) doWithIdempotencyKey(ctx context.Context, key string, fn func() (*Result, error)) (*Result, error) {
	// Runner 统一负责进程内单飞；幂等结果读写由 store 负责。
	call, owner, err := r.beginIdempotencyCall(ctx, key)
	if err != nil {
		return nil, err
	}
	return singleflight.Run(ctx, call, owner,
		waitRunnerIdempotencyCall,
		func(call *singleflight.Call[*Result], result *Result, err error) {
			r.finishIdempotencyCall(key, call, result, err)
		},
		func(recovered any) error { return idempotencyPanicError(recovered) },
		fn,
	)
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
