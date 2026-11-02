package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"

	"gochen/auth"
	"gochen/contextx"
	"gochen/errors"
)

// SupportsTransfer 报告入口能否持久化幂等结果并预占并发请求。
func (r *Runner) SupportsTransfer() bool {
	if r == nil {
		return false
	}
	_, tracked := r.store.(IIdempotencyStore)
	_, reserved := r.store.(IIdempotencyReservationStore)
	return tracked && reserved
}

// ExecuteTransfer 供 REST 和命令入口共用，版本及幂等身份必须由调用方提供。
func (r *Runner) ExecuteTransfer(ctx context.Context, spec *Spec, expectedVersion *uint64, fn func(context.Context, uint64) (*Result, error)) (*Result, error) {
	if ctx == nil || spec == nil || spec.Mode != ModeTracked || strings.TrimSpace(spec.IdempotencyKey) == "" || expectedVersion == nil || *expectedVersion == math.MaxUint64 || fn == nil {
		return nil, errors.NewCode(errors.InvalidInput, "transfer requires tracked mode, idempotency key, expected version and handler")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.Resource == nil || strings.TrimSpace(spec.Resource.Type) == "" || strings.TrimSpace(spec.Resource.ID) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "transfer requires resource type and identity")
	}
	subject := contextx.Operator(ctx)
	if principal, ok := auth.PrincipalFromContext(ctx); ok {
		subject = principal.SubjectID
	}
	if strings.TrimSpace(subject) == "" {
		return nil, errors.NewCode(errors.InvalidInput, "transfer request identity is required")
	}
	if !r.SupportsTransfer() {
		return nil, errors.NewCode(errors.InvalidInput, "transfer runner requires idempotency storage and reservations")
	}
	identity, err := json.Marshal([]string{contextx.TenantID(ctx), subject, spec.Type, spec.Resource.Type, spec.Resource.ID, spec.IdempotencyKey})
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "encode transfer identity")
	}
	digest := sha256.Sum256(identity)
	scoped := *spec
	scoped.IdempotencyKey = hex.EncodeToString(digest[:])
	version := *expectedVersion
	return r.Execute(ctx, &scoped, func(ctx context.Context) (*Result, error) { return fn(ctx, version) })
}
