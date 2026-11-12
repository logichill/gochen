package app

import (
	"context"
	"sync/atomic"

	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// CommandResource 声明命令实际访问的资源、动作、有效契约与请求意图。
type CommandResource[Input any] struct {
	Name       string
	Repository IExecutionDescriptorProvider
	Authorizer *auth.Authorizer
	Action     string
	Intent     func(Input) (auth.Request, error)
	// ConditionPath 在专属资源能力装配时记录实际条件执行路径。
	ConditionPath string
	// ReuseFactsFrom 指定已授权前序资源，在相同目标意图中复用共同提供方的已加载投影。
	ReuseFactsFrom string
	// When 在已授权资源的可信事实内决定是否访问当前资源；零值表示始终执行。
	When func(context.Context, Input, []*CommandGrant) (bool, error)
}

// CommandOptions 连接强类型输入、资源声明、受约束访问能力与业务函数。
// Access 在全部资源授权通过后构造；Handler 通过 Access 操作资源。
type CommandOptions[Input, Access, Output any] struct {
	Resources []CommandResource[Input]
	Access    func(context.Context, Input, []*CommandGrant) (Access, error)
	Handler   func(context.Context, Input, Access) (Output, error)
}

// Command 在业务副作用开始前完成全部资源授权及执行契约校验。
type Command[Input, Access, Output any] struct {
	options    CommandOptions[Input, Access, Output]
	executions []ExecutionDescriptor
}

// NewCommand 冻结声明并校验资源有效契约，业务处理函数仅消费受约束访问能力。
func NewCommand[Input, Access, Output any](options CommandOptions[Input, Access, Output]) (*Command[Input, Access, Output], error) {
	if len(options.Resources) == 0 || options.Access == nil || options.Handler == nil {
		return nil, errors.NewCode(errors.InvalidInput, "command requires resources, access assembly and handler")
	}
	options.Resources = append([]CommandResource[Input](nil), options.Resources...)
	seen := map[string]bool{}
	executions := make([]ExecutionDescriptor, 0, len(options.Resources))
	for _, resource := range options.Resources {
		if resource.Name == "" || seen[resource.Name] || depcheck.IsNil(resource.Repository) || resource.Authorizer == nil || resource.Intent == nil {
			return nil, errors.NewCode(errors.InvalidInput, "invalid or duplicate command resource").WithContext("resource", resource.Name)
		}
		if resource.ReuseFactsFrom != "" && !seen[resource.ReuseFactsFrom] {
			return nil, errors.NewCode(errors.InvalidInput, "fact reuse requires a preceding command resource").WithContext("resource", resource.Name)
		}
		seen[resource.Name] = true
		plan, bound := resource.Authorizer.Plan()
		if !bound || plan.ResourceAccess != auth.ResourceAccessData {
			return nil, errors.NewCode(errors.InvalidInput, "resource command requires a compiled data access plan")
		}
		action, ok := plan.Descriptor.Action(resource.Action)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "command action is absent from authorization plan")
		}
		switch action.Kind {
		case auth.KindRead, auth.KindList, auth.KindCreate, auth.KindUpdate, auth.KindDelete:
		default:
			return nil, errors.NewCode(errors.InvalidInput, "command requires a supported resource action kind")
		}
		execution := resource.Repository.ExecutionDescriptor()
		if execution.Metadata() == nil {
			return nil, errors.NewCode(errors.InvalidInput, "command repository requires bound entity metadata")
		}
		if err := execution.ValidateVocabulary(resource.Authorizer.Vocabulary()); err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	return &Command[Input, Access, Output]{options: options, executions: executions}, nil
}

// Execute 授权所有资源后运行受保护业务函数，结束时撤销访问能力。
func (c *Command[Input, Access, Output]) Execute(ctx context.Context, input Input) (Output, error) {
	var zero Output
	if ctx == nil {
		return zero, errors.NewCode(errors.InvalidInput, "command context is required")
	}
	frozen, err := meta.Clone(input)
	if err != nil {
		return zero, err
	}
	grants := make([]*CommandGrant, 0, len(c.options.Resources))
	requests := make([]auth.Request, 0, len(c.options.Resources))
	defer func() {
		for _, grant := range grants {
			if grant != nil {
				grant.closed.Store(true)
			}
		}
		for _, request := range requests {
			request.CloseFacts()
		}
	}()
	for index, resource := range c.options.Resources {
		// 每个意图投影读取独立输入，避免装配函数修改后续资源的意图。
		projection, err := meta.Clone(frozen)
		if err != nil {
			return zero, err
		}
		if resource.When != nil {
			enabled, err := resource.When(ctx, projection, append([]*CommandGrant(nil), grants...))
			if err != nil {
				return zero, err
			}
			if !enabled {
				grants = append(grants, nil)
				continue
			}
			projection, err = meta.Clone(frozen)
			if err != nil {
				return zero, err
			}
		}
		request, err := resource.Intent(projection)
		if err != nil {
			return zero, err
		}
		plan, _ := resource.Authorizer.Plan()
		if request.Resource != "" && request.Resource != plan.Descriptor.Resource || request.Action != "" && request.Action != resource.Action {
			return zero, errors.NewCode(errors.InvalidInput, "command intent conflicts with resource declaration")
		}
		request.Resource = plan.Descriptor.Resource
		request.Action = resource.Action
		if resource.ReuseFactsFrom != "" {
			var previous *CommandGrant
			for _, grant := range grants {
				if grant != nil && grant.name == resource.ReuseFactsFrom {
					previous = grant
					break
				}
			}
			if previous == nil {
				return zero, errors.NewCode(errors.InvalidInput, "fact reuse source did not execute")
			}
			request, err = resource.Authorizer.ReuseFacts(ctx, request, previous.request)
			if err != nil {
				return zero, err
			}
		}
		decision, managed, err := resource.Authorizer.AuthorizeRequest(ctx, request)
		requests = append(requests, managed)
		if err != nil {
			return zero, err
		}
		if !decision.Allowed {
			return zero, errors.NewCode(errors.Forbidden, "command authorization denied").WithContext("resource", resource.Name).WithContext("reason", decision.Reason)
		}
		action, _ := plan.Descriptor.Action(resource.Action)
		if decision.Spec.IsNone() && action.Kind != auth.KindRead && action.Kind != auth.KindList {
			return zero, errors.NewCode(errors.Forbidden, "command write scope is empty").WithContext("resource", resource.Name)
		}
		execution := c.executions[index]
		if err := decision.ValidateAgainst(resource.Authorizer.Vocabulary(), execution.Metadata()); err != nil {
			return zero, err
		}
		grants = append(grants, &CommandGrant{name: resource.Name, repository: resource.Repository, authorizer: resource.Authorizer, execution: execution, request: managed, decision: decision, action: action, bound: ctx})
	}
	assemblyInput, err := meta.Clone(frozen)
	if err != nil {
		return zero, err
	}
	access, err := c.options.Access(ctx, assemblyInput, grants)
	if err != nil {
		return zero, err
	}
	return c.options.Handler(ctx, frozen, access)
}

// CommandGrant 是一次执行期间的受管理授权结果；访问能力由组合根绑定。
type CommandGrant struct {
	authorizer *auth.Authorizer
	name       string
	repository IExecutionDescriptorProvider
	execution  ExecutionDescriptor
	request    auth.Request
	decision   auth.Decision
	action     auth.Action
	bound      context.Context
	closed     atomic.Bool
}

// Name 返回命令资源声明名称。
func (g *CommandGrant) Name() string { return g.name }

// Request 返回冻结意图的独立副本，供已注册事实提供方复用。
func (g *CommandGrant) Request() (auth.Request, error) {
	if g == nil || g.closed.Load() {
		return auth.Request{}, errors.NewCode(errors.FailedPrecondition, "command access is closed")
	}
	return g.request.Copy()
}

func (g *CommandGrant) check(ctx context.Context) error {
	if g == nil || g.closed.Load() {
		return errors.NewCode(errors.FailedPrecondition, "command access is closed")
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	bound, present := auth.PrincipalFromContext(g.bound)
	if principal != bound || ok != present || !contextx.HasSameTenantBinding(ctx, g.bound) || (present && !auth.HasSamePrincipalBinding(ctx, g.bound)) {
		return errors.NewCode(errors.Forbidden, "command access identity binding changed")
	}
	return nil
}

// AuthorizationBinding 返回同一入口全部资源的执行绑定，供 HTTP、命令或 Worker 清单核对。
func (c *Command[Input, Access, Output]) AuthorizationBinding() (auth.EntryBinding, error) {
	var primary auth.EntryBinding
	for i, resource := range c.options.Resources {
		plan, _ := resource.Authorizer.Plan()
		binding, err := auth.BindExecution(resource.Authorizer, plan.Descriptor, auth.ResourceAccessData, "app.Command resource "+resource.Name, "CommandRepository -> Repository(spec, protected fields, version)", resource.Action)
		if err != nil {
			return auth.EntryBinding{}, err
		}
		if resource.ConditionPath != "" {
			binding.ConditionPath = resource.ConditionPath
		}
		if resource.When != nil {
			binding.ConditionPath = "When(trusted resource facts) -> " + binding.ConditionPath
		}
		if i == 0 {
			primary = binding
		} else {
			primary.Resources = append(primary.Resources, auth.ResourceBinding{Name: resource.Name, Plan: binding.Plan, Action: binding.Action, Source: binding.Source, ConditionPath: binding.ConditionPath})
		}
	}
	return primary, nil
}
