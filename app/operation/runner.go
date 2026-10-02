package operation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gochen/app/operation/internal/singleflight"
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

const idempotencyReservationCleanupTimeout = 5 * time.Second

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
		defer r.releaseIdempotencyReservationOnPanic(operationID, cfg)
		execCtx = WithOperationID(execCtx, operationID)
	}

	result, err := fn(execCtx)
	if err != nil {
		failed := r.failedResult(spec, operationID, result, err)
		if spec.Mode != ModeTracked {
			return failed, err
		}
		if stored, replayed, storeErr := r.storeResult(execCtx, failed, cfg); storeErr != nil {
			_ = r.releaseIdempotencyReservation(ctx, operationID, cfg)
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
			_ = r.releaseIdempotencyReservation(ctx, operationID, cfg)
			return merged, err
		}
		return stored, nil
	}
	return merged, nil
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

func (r *Runner) releaseIdempotencyReservationOnPanic(operationID string, cfg executeConfig) {
	if recovered := recover(); recovered != nil {
		func() {
			defer func() {
				_ = recover()
			}()
			_ = r.releaseIdempotencyReservation(context.Background(), operationID, cfg)
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

func (r *Runner) failedResult(spec *Spec, operationID string, result *Result, err error) *Result {
	return r.buildMergedResult(spec, operationID, result, StatusFailed, operationErrorFromError(err))
}

func (r *Runner) buildMergedResult(spec *Spec, operationID string, result *Result, status Status, opErr *OperationError) *Result {
	if result == nil {
		result = &Result{}
	}
	result = CloneResult(result)
	if opErr == nil {
		opErr = result.Error
	}
	merged := &Result{
		Resource:       mergeResource(result.Resource, spec.Resource),
		Result:         result.Result,
		Error:          opErr,
		AffectedScopes: mergeScopes(spec.AffectedScopes, result.AffectedScopes),
		StatusURL:      result.StatusURL,
		StreamURL:      result.StreamURL,
		RetryAfterMs:   result.RetryAfterMs,
	}
	merged.Operation = Operation{
		ID:     operationID,
		Type:   spec.Type,
		Mode:   spec.Mode,
		Status: status,
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
	}
	return merged
}

func defaultStatusForMode(mode Mode) Status {
	if mode == ModeTracked {
		return StatusAccepted
	}
	return StatusSettled
}

func successStatusForResult(result *Result, mode Mode) Status {
	if mode == ModeInline {
		return StatusSettled
	}
	if result != nil && result.Operation.Status.IsValid() && result.Operation.Status != StatusFailed {
		return result.Operation.Status
	}
	return defaultStatusForMode(mode)
}

func operationErrorFromError(err error) *OperationError {
	if err == nil {
		return nil
	}
	code := errors.Code(err)
	if code == "" {
		code = errors.Internal
	}
	message := publicOperationErrorMessage(code)
	var appErr *errors.AppError
	var details map[string]any
	if errors.As(err, &appErr) && appErr != nil {
		message = appErr.Message()
		details = sanitizeOperationErrorDetails(appErr.Details())
	}
	return &OperationError{
		Code:    string(code),
		Message: message,
		Details: details,
	}
}

func publicOperationErrorMessage(code errors.ErrorCode) string {
	if message := errors.PublicMessage(code); message != "" {
		return message
	}
	return "operation failed"
}

func sanitizeOperationErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]any, len(details))
	for key, value := range details {
		if isSensitiveOperationErrorDetail(key) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSensitiveOperationErrorDetail(key string) bool {
	switch strings.TrimSpace(strings.ToLower(key)) {
	case "stack", "panic", "panic_stack", "cause", "error", "err", "raw_error":
		return true
	default:
		return false
	}
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
