package operation

import (
	"context"
	"strings"

	"gochen/contextx"
	"gochen/errors"
	"gochen/internal/depcheck"
)

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
