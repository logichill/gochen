package operation

import (
	"context"

	"gochen/errors"
)

// ISettlementChecker 只负责回答“该 tracked operation 是否已经对外收敛可见”。
// 具体判定规则由业务层实现并注入，框架仅负责调用与状态推进。
type ISettlementChecker interface {
	IsSettled(ctx context.Context, result *Result) (bool, error)
}

// SettlementCheckerFunc 允许直接用函数注入 settlement policy。
type SettlementCheckerFunc func(ctx context.Context, result *Result) (bool, error)

// IsSettled 实现 ISettlementChecker。
func (f SettlementCheckerFunc) IsSettled(ctx context.Context, result *Result) (bool, error) {
	if f == nil {
		return false, nil
	}
	return f(ctx, result)
}

// TrackerOptions 描述 tracked operation 的通用协调依赖。
type TrackerOptions struct {
	Store      IStore
	Settlement ISettlementChecker
}

// Tracker 负责 tracked operation 的最小状态推进：
// - 从 store 读取；
// - 调用业务 settlement checker；
// - 把 accepted 推进到 processing；
// - 在收敛后写回 settled。
type Tracker struct {
	store      IStore
	settlement ISettlementChecker
}

// NewTracker 创建通用 tracker。
func NewTracker(opts *TrackerOptions) *Tracker {
	if opts == nil {
		return &Tracker{}
	}
	return &Tracker{
		store:      opts.Store,
		settlement: opts.Settlement,
	}
}

// Load 从 store 读取并刷新当前 operation 状态。
func (t *Tracker) Load(ctx context.Context, operationID string) (*Result, error) {
	if operationID == "" {
		return nil, errors.NewCode(errors.InvalidInput, "operation id is required")
	}
	if t == nil || t.store == nil {
		return nil, errors.NewCode(errors.Internal, "operation tracker store is not configured")
	}
	result, err := t.store.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}
	return t.Refresh(ctx, result)
}

// Refresh 依据 settlement checker 推进 tracked operation 的可观察状态。
func (t *Tracker) Refresh(ctx context.Context, result *Result) (*Result, error) {
	if result == nil {
		return nil, errors.NewCode(errors.InvalidInput, "operation result cannot be nil")
	}
	if IsTerminalStatus(result.Operation.Status) {
		return result, nil
	}

	settled := false
	var err error
	if t != nil && t.settlement != nil {
		settled, err = t.settlement.IsSettled(ctx, result)
		if err != nil {
			return nil, err
		}
	}

	next := CloneResult(result)
	if settled {
		next.Operation.Status = StatusSettled
	} else if next.Operation.Status == StatusAccepted {
		next.Operation.Status = StatusProcessing
	}

	if t != nil && t.store != nil && next.Operation.ID != "" {
		if err := t.store.Put(ctx, next); err != nil {
			return nil, err
		}
	}
	return next, nil
}

func IsTerminalStatus(status Status) bool {
	return status == StatusSettled ||
		status == StatusFailed ||
		status == StatusTimeout ||
		status == StatusDegraded
}
