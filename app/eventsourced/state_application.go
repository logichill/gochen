package eventsourced

import (
	"context"
	"reflect"

	"gochen/app"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// StateOptions 将显式状态映射到强类型授权事实 V；V 必须是带 entity:"id"
// 字段的结构体指针，ID 类型与事件流一致。状态 S 不要求实体接口或反射标签。
// View 属于可信装配，必须完整表达策略及受保护维度，不能使用客户端值伪造事实。
// Validate 只能验证状态；其参数是独立副本，修改不会进入事件流。
type StateOptions[S any, ID comparable, V any] struct {
	Resource   string
	Authorizer auth.IAuthorizer
	Actions    []auth.Action
	View       func(ID, S) V
	Contract   app.ResourceContract
	// Isolation 与 Contract.Isolation 必须同时配置，从可信上下文解析隔离值。
	// 不配置时，租户或部门等数据范围由 Authorizer 的 Spec 明确表达。
	Isolation func(context.Context) (any, error)
	Validate  func(S) error
}

// StateApplication 是显式 ES 的受保护入口。所有读取及追加经过统一授权；
// 业务只决定事件，不能通过回调直接调用本应用的底层提交端口。
// 低层仓储仍须由组合根持有，不应另外注入业务服务。
type StateApplication[S any, E domain.IDomainEvent, ID comparable] struct {
	repo       *StateRepository[S, E, ID]
	authorizer auth.IAuthorizer
	descriptor auth.Descriptor
	actions    map[string]auth.Action
	vocabulary auth.Vocabulary
	execution  app.ExecutionDescriptor
	view       func(ID, S) (any, error)
	isolation  func(context.Context) (any, error)
	validate   func(S) error
}

// DescribeState 返回与构造期同源的显式动作清单，不隐式增加 CRUD 动作。
func DescribeState[S any, ID comparable, V any](options StateOptions[S, ID, V]) (auth.Descriptor, error) {
	descriptor, _, err := stateDescriptor(options.Resource, options.Actions)
	return descriptor, err
}

func stateDescriptor(resource string, actions []auth.Action) (auth.Descriptor, map[string]auth.Action, error) {
	if len(actions) == 0 {
		return auth.Descriptor{}, nil, errors.NewCode(errors.InvalidInput, "state application requires explicit actions")
	}
	for _, action := range actions {
		if action.Kind != auth.KindRead && action.Kind != auth.KindCreate && action.Kind != auth.KindUpdate {
			return auth.Descriptor{}, nil, errors.NewCode(errors.InvalidInput, "state application supports read, create and update actions")
		}
	}
	return execution.BuildDescriptor(resource, nil, actions)
}

// NewStateApplication 校验动作、事实类型、授权计划及资源契约后冻结装配。
func NewStateApplication[S any, E domain.IDomainEvent, ID comparable, V any](repo *StateRepository[S, E, ID], options StateOptions[S, ID, V]) (*StateApplication[S, E, ID], error) {
	if repo == nil || depcheck.IsNil(options.Authorizer) || options.View == nil {
		return nil, errors.NewCode(errors.InvalidInput, "state application requires repository, authorizer and view")
	}
	if err := execution.ValidateIDType[ID](); err != nil {
		return nil, err
	}
	metadata, err := meta.Describe[V]()
	if err != nil {
		return nil, err
	}
	idField, ok := metadata.IDField()
	if !ok || idField.Type != reflect.TypeFor[ID]() {
		return nil, errors.NewCode(errors.InvalidInput, "state view requires the event stream ID type")
	}
	descriptor, actions, err := stateDescriptor(options.Resource, options.Actions)
	if err != nil {
		return nil, err
	}
	if err := auth.ValidatePlanBinding(options.Authorizer, descriptor, auth.ResourceAccessData); err != nil {
		return nil, err
	}
	effective, err := app.BindContract(metadata, options.Contract)
	if err != nil {
		return nil, err
	}
	vocabulary := options.Authorizer.Vocabulary().Copy()
	if err := effective.ValidateVocabulary(vocabulary); err != nil {
		return nil, err
	}
	if (effective.IsolationField() != "") != (options.Isolation != nil) {
		return nil, errors.NewCode(errors.InvalidInput, "state isolation requires both a field and trusted resolver")
	}
	projector := options.View
	view := func(id ID, state S) (any, error) {
		projected := projector(id, state)
		if depcheck.IsNil(projected) {
			return nil, errors.NewCode(errors.InvalidInput, "state view cannot be nil")
		}
		frozen, err := meta.Clone(projected)
		if err != nil {
			return nil, err
		}
		actual, err := idField.Read(frozen)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(actual, id) {
			return nil, errors.NewCode(errors.Forbidden, "state view differs from the target event stream")
		}
		return frozen, nil
	}
	return &StateApplication[S, E, ID]{repo: repo, authorizer: options.Authorizer, descriptor: descriptor, actions: actions, vocabulary: vocabulary, execution: effective, view: view, isolation: options.Isolation, validate: options.Validate}, nil
}

// Descriptor 返回独立动作快照。
func (a *StateApplication[S, E, ID]) Descriptor() auth.Descriptor { return a.descriptor.Copy() }

// ExecutionDescriptor 返回与事实视图绑定的有效资源契约。
func (a *StateApplication[S, E, ID]) ExecutionDescriptor() app.ExecutionDescriptor {
	return a.execution
}

// AuthorizationPlan 返回与执行入口绑定的授权计划。
func (a *StateApplication[S, E, ID]) AuthorizationPlan() (auth.Plan, bool) {
	return auth.PlanOf(a.authorizer)
}

// UsesAllowAuthorizer 报告显式放行配置，供启动检查使用。
func (a *StateApplication[S, E, ID]) UsesAllowAuthorizer() bool { return auth.IsAllow(a.authorizer) }

// AuthorizationBinding 绑定本对象实际执行的动作及提交前检查路径。
func (a *StateApplication[S, E, ID]) AuthorizationBinding(action string) (auth.EntryBinding, error) {
	if a == nil || a.repo == nil {
		return auth.EntryBinding{}, errors.NewCode(errors.InvalidInput, "state application is not initialized")
	}
	return auth.BindExecution(a.authorizer, a.descriptor, auth.ResourceAccessData, "app/eventsourced.StateApplication", "Authorize -> Replay -> Match -> Decide -> Replay -> Check -> Append(expectedVersion)", action)
}

// FastDeny 执行可安全提前拒绝的检查，不能代替正式授权。
func (a *StateApplication[S, E, ID]) FastDeny(ctx context.Context, action string) error {
	return auth.CheckFastDeny(ctx, a.authorizer, action)
}
