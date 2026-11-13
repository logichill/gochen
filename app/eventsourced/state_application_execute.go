package eventsourced

import (
	"context"
	"reflect"
	"strings"

	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// Load 在授权后回放，验证当前状态的范围和隔离边界后才返回独立状态。
// 版本零表示没有已提交事件，对受保护读取返回 NotFound。
func (a *StateApplication[S, E, ID]) Load(ctx context.Context, action string, id ID) (*State[S, ID], error) {
	if err := a.ready(ctx, id); err != nil {
		return nil, err
	}
	isolation, err := a.isolationValue(ctx)
	if err != nil {
		return nil, err
	}
	s, _, err := a.authorize(ctx, action, id, auth.KindRead)
	if err != nil {
		return nil, err
	}
	base, err := a.repo.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if base.Version() == 0 {
		return nil, errors.NewCode(errors.NotFound, "event stream has no committed state")
	}
	view, err := a.view(id, base.Value())
	if err != nil {
		return nil, err
	}
	if err := a.match(view, s, isolation, errors.NotFound); err != nil {
		return nil, err
	}
	return base, nil
}

// Execute 授权、回放并校验客户端版本，然后决定事件；真实归约结果必须通过
// 范围、隔离、受保护字段、权威赋值和领域校验后才可追加。不会自动重试。
// create 只接受版本零（没有已提交事件），update 必须已有状态。
// 空事件仍执行所有结果校验，但低层不追加，也不提供额外原子版本守卫。
func (a *StateApplication[S, E, ID]) Execute(ctx context.Context, action string, id ID, expectedVersion uint64, decide func(context.Context, S) ([]E, error), opts ...ExecOption) (*State[S, ID], error) {
	if err := a.ready(ctx, id); err != nil {
		return nil, err
	}
	if decide == nil {
		return nil, errors.NewCode(errors.InvalidInput, "state decision function is required")
	}
	isolation, err := a.isolationValue(ctx)
	if err != nil {
		return nil, err
	}
	s, decision, err := a.authorize(ctx, action, id, auth.KindCreate, auth.KindUpdate)
	if err != nil {
		return nil, err
	}
	// 显式流 ID 在调用前已确定，并经 View 严格绑定；与生成 ID 的 CRUD 创建不同，
	// 此处可安全执行引用流 ID 的纯写谓词，但仍拒绝命名条件。
	if err := a.execution.ValidateWriteSpec(s, false); err != nil {
		return nil, err
	}
	if s.IsNone() {
		return nil, errors.NewCode(errors.Forbidden, "write has no authorized scope")
	}
	creating := a.actions[action].Kind == auth.KindCreate
	if creating && expectedVersion != 0 {
		return nil, errors.NewCode(errors.InvalidInput, "create requires expected version zero")
	}
	runCtx, err := a.commandContext(ctx, action, opts)
	if err != nil {
		return nil, err
	}
	base, err := a.repo.Load(runCtx, id)
	if err != nil {
		return nil, err
	}
	if !creating && base.Version() == 0 {
		return nil, errors.NewCode(errors.NotFound, "update requires committed state")
	}
	var protected map[string]any
	if base.Version() > 0 {
		view, err := a.view(id, base.Value())
		if err != nil {
			return nil, err
		}
		if err := a.match(view, s, isolation, errors.NotFound); err != nil {
			return nil, err
		}
		if !creating {
			protected, err = a.execution.CaptureProtected(view)
			if err != nil {
				return nil, err
			}
			protected, err = meta.Clone(protected)
			if err != nil {
				return nil, err
			}
		}
	}
	// A hidden stream must not disclose its existence or version through conflicts.
	if base.Version() != expectedVersion {
		return nil, errors.NewCode(errors.Concurrency, "event stream expected version is stale")
	}
	guard := func(next S) error {
		view, err := a.view(id, a.repo.model.Clone(next))
		if err != nil {
			return err
		}
		if !creating {
			if err := a.execution.CheckProtected(protected, view); err != nil {
				return err
			}
		}
		if err := execution.ValidateAssignments(view, decision.Assign, a.execution.Metadata()); err != nil {
			return err
		}
		if validatable, ok := any(a.repo.model.Clone(next)).(domain.IValidatable); ok && !depcheck.IsNil(validatable) {
			if err := validatable.Validate(); err != nil {
				return err
			}
		}
		if a.validate != nil {
			if err := a.validate(a.repo.model.Clone(next)); err != nil {
				return err
			}
		}
		return a.match(view, s, isolation, errors.Forbidden)
	}
	return a.repo.execute(runCtx, base, decide, guard)
}

func (a *StateApplication[S, E, ID]) ready(ctx context.Context, id ID) error {
	if err := stateContext(ctx); err != nil {
		return err
	}
	if a == nil || a.repo == nil || a.view == nil {
		return errors.NewCode(errors.InvalidInput, "state application is not initialized")
	}
	v := reflect.ValueOf(id)
	switch v.Kind() {
	case reflect.String:
		if v.String() == "" || strings.TrimSpace(v.String()) != v.String() {
			return errors.NewCode(errors.InvalidInput, "event stream ID must be nonempty and canonical")
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() <= 0 {
			return errors.NewCode(errors.InvalidInput, "event stream ID must be positive")
		}
	default:
		if v.Uint() == 0 {
			return errors.NewCode(errors.InvalidInput, "event stream ID must be positive")
		}
	}
	return nil
}
func (a *StateApplication[S, E, ID]) authorize(ctx context.Context, action string, id ID, kinds ...auth.Kind) (spec.Spec, auth.Decision, error) {
	return execution.AuthorizeKind(ctx, a.descriptor.Resource, a.authorizer, a.descriptor, a.actions, a.vocabulary, a.execution.Metadata(), action, []string{execution.FormatID(id)}, kinds...)
}
func (a *StateApplication[S, E, ID]) isolationValue(ctx context.Context) (any, error) {
	if a.isolation == nil {
		return nil, nil
	}
	value, err := a.isolation(ctx)
	if err != nil {
		return nil, err
	}
	if depcheck.IsNil(value) {
		return nil, errors.NewCode(errors.Unauthorized, "state isolation value is required")
	}
	field, _ := a.execution.Metadata().Field(a.execution.IsolationField())
	converted, err := field.Convert(value)
	if err != nil {
		return nil, err
	}
	// Field.Read compares the scalar behind pointer/interface fields, not their
	// Go representation. Normalize the trusted value identically; NULL cannot
	// establish an isolation boundary.
	for converted.Kind() == reflect.Pointer || converted.Kind() == reflect.Interface {
		if converted.IsNil() {
			return nil, errors.NewCode(errors.Unauthorized, "state isolation value is required")
		}
		converted = converted.Elem()
	}
	return meta.Clone(converted.Interface())
}
func (a *StateApplication[S, E, ID]) match(view any, s spec.Spec, isolation any, code errors.ErrorCode) error {
	if a.isolation != nil {
		field, _ := a.execution.Metadata().Field(a.execution.IsolationField())
		actual, err := field.Read(view)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, isolation) {
			return errors.NewCode(code, "state is outside the isolation boundary")
		}
	}
	allowed, err := spec.NewInterpreter().Match(s, view)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.NewCode(code, "state is outside the authorized scope")
	}
	return nil
}
func (a *StateApplication[S, E, ID]) commandContext(ctx context.Context, action string, opts []ExecOption) (context.Context, error) {
	actor, err := execution.ResolveActor(ctx, opts, false)
	if err != nil {
		return nil, err
	}
	if actor.Operator != "" {
		ctx, err = contextx.WithOperator(ctx, actor.Operator)
		if err != nil {
			return nil, err
		}
	}
	return contextx.WithEventAudit(ctx, contextx.EventAudit{Operator: actor.Operator, Initiator: actor.Initiator, Action: action, Reason: actor.Reason, CorrelationID: actor.CorrelationID}), nil
}
