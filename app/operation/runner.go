package operation

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"gochen/errors"
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

type runner struct {
	idGenerator         func() string
	statusURLBuilder    func(operationID string) string
	streamURLBuilder    func(operationID string) string
	defaultRetryAfterMs int
	store               IStore
}

type contextKey string

const operationIDContextKey contextKey = "gochen/app/operation/id"

var (
	defaultRunner IRunner = NewRunner(nil)
	operationSeq  uint64
)

// DefaultRunner 返回框架默认的轻量 runner。
func DefaultRunner() IRunner {
	return defaultRunner
}

// NewRunner 创建最小可用的 operation runner。
func NewRunner(opts *RunnerOptions) IRunner {
	r := &runner{
		idGenerator: defaultOperationID,
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
func (r *runner) Execute(ctx context.Context, spec *Spec, fn Handler) (*Result, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, errors.NewCode(errors.InvalidInput, "operation handler cannot be nil")
	}

	operationID := ""
	execCtx := ctx
	if spec.Mode == ModeTracked {
		operationID = strings.TrimSpace(r.idGenerator())
		if operationID == "" {
			return nil, errors.NewCode(errors.Internal, "tracked operation id cannot be empty")
		}
		execCtx = WithOperationID(execCtx, operationID)
	}

	result, err := fn(execCtx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &Result{}
	}

	merged := &Result{
		Resource:       firstNonNilResource(result.Resource, spec.Resource),
		Result:         result.Result,
		Error:          result.Error,
		AffectedScopes: mergeScopes(spec.AffectedScopes, result.AffectedScopes),
		StatusURL:      result.StatusURL,
		StreamURL:      result.StreamURL,
		RetryAfterMs:   result.RetryAfterMs,
	}
	merged.Operation = Operation{
		ID:     result.Operation.ID,
		Type:   spec.Type,
		Mode:   spec.Mode,
		Status: result.Operation.Status,
	}

	if merged.Operation.ID == "" {
		merged.Operation.ID = operationID
	}
	if !merged.Operation.Status.IsValid() {
		if spec.Mode == ModeTracked {
			merged.Operation.Status = StatusAccepted
		} else {
			merged.Operation.Status = StatusSettled
		}
	}
	if spec.Mode == ModeTracked {
		if merged.StatusURL == "" && r.statusURLBuilder != nil {
			merged.StatusURL = r.statusURLBuilder(merged.Operation.ID)
		}
		if merged.StreamURL == "" && r.streamURLBuilder != nil {
			merged.StreamURL = r.streamURLBuilder(merged.Operation.ID)
		}
		if merged.RetryAfterMs == 0 {
			merged.RetryAfterMs = r.defaultRetryAfterMs
		}
		if r.store != nil {
			if err := r.store.Put(execCtx, merged); err != nil {
				return nil, err
			}
		}
	}
	return merged, nil
}

func defaultOperationID() string {
	seq := atomic.AddUint64(&operationSeq, 1)
	return fmt.Sprintf("op_%d_%d", time.Now().UnixNano(), seq)
}

func firstNonNilResource(resources ...*Resource) *Resource {
	for _, resource := range resources {
		if resource != nil {
			copyValue := *resource
			return &copyValue
		}
	}
	return nil
}

func mergeScopes(groups ...[]string) []string {
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, group := range groups {
		for _, scope := range group {
			if scope == "" {
				continue
			}
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			merged = append(merged, scope)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
