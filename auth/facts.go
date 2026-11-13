package auth

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// Copy 隔离请求意图中的可变值；可信事实会话由执行内核管理。
func (r Request) Copy() (Request, error) {
	r.IDs = append([]string(nil), r.IDs...)
	if r.Targets != nil {
		targets, err := meta.Clone(r.Targets)
		if err != nil {
			return Request{}, err
		}
		r.Targets = targets
	}
	return r, nil
}

// IFactSource 是装配期注册的可信投影提供方，由 NewFact 或 NewDerivedFact 创建。
// 提供方从仓储等可信来源读取最小投影，客户端字段仅作为请求意图。
type IFactSource interface {
	FactName() string
	loadFact(context.Context, Request) (any, error)
}

// IFactConsumer 显式声明策略或范围解析器可能使用的事实依赖。
type IFactConsumer interface {
	FactSources() []IFactSource
}

// Fact 以 Go 类型约束可信投影；每个请求内按需读取一次并返回独立副本。
type Fact[T any] struct {
	name         string
	load         func(context.Context, Request) (T, error)
	dependencies []IFactSource
}

// NewFact 装配可信读取函数；同一授权器内投影名称必须唯一。
func NewFact[T any](name string, load func(context.Context, Request) (T, error)) (*Fact[T], error) {
	name = strings.TrimSpace(name)
	if name == "" || load == nil {
		return nil, errors.NewCode(errors.InvalidInput, "fact requires a name and trusted loader")
	}
	return &Fact[T]{name: name, load: load}, nil
}

// NewDerivedFact 从已注册父投影按需解析关联事实，父投影沿用同一会话的冻结值。
// 依赖在构造时固定，编译器递归登记；读取函数仅能访问声明的父投影。
func NewDerivedFact[Parent, T any](name string, parent *Fact[Parent], load func(context.Context, Request, Parent) (T, error)) (*Fact[T], error) {
	if parent == nil || parent.name == "" || parent.load == nil || load == nil {
		return nil, errors.NewCode(errors.InvalidInput, "derived fact requires a constructed parent and loader")
	}
	fact, err := NewFact(name, func(ctx context.Context, request Request) (T, error) {
		var zero T
		value, err := parent.Get(ctx, request)
		if err != nil {
			return zero, err
		}
		request.facts = nil
		return load(ctx, request, value)
	})
	if err != nil {
		return nil, err
	}
	fact.dependencies = []IFactSource{parent}
	return fact, nil
}

func (f *Fact[T]) FactName() string                { return f.name }
func (f *Fact[T]) factDependencies() []IFactSource { return f.dependencies }
func (f *Fact[T]) loadFact(ctx context.Context, req Request) (any, error) {
	return f.load(ctx, req)
}

// Get 仅在同一执行内核、主体绑定和冻结意图内读取已注册事实。
func (f *Fact[T]) Get(ctx context.Context, req Request) (T, error) {
	var zero T
	if f == nil || req.facts == nil {
		return zero, errors.NewCode(errors.InvalidInput, "fact requires a managed authorization request")
	}
	value, err := req.facts.get(ctx, req, f)
	if err != nil {
		return zero, err
	}
	projection, ok := value.(T)
	if !ok {
		// nil 接口是合法的空投影。
		if value == nil && reflect.TypeOf((*T)(nil)).Elem().Kind() == reflect.Interface {
			return zero, nil
		}
		return zero, errors.NewCode(errors.Internal, "fact projection has an unexpected type")
	}
	return projection, nil
}

type factResult struct {
	once  sync.Once
	ready atomic.Bool
	value any
	err   error
}

type factSession struct {
	owner   *Authorizer
	bound   context.Context
	intent  Request
	results map[IFactSource]*factResult
	closed  atomic.Bool
}

// CloseFacts 撤销请求中的事实读取能力；已取得的独立投影可继续使用。
// 多资源执行骨架在完成或失败时关闭各请求会话，本方法可重复调用。
func (r Request) CloseFacts() {
	if r.facts != nil {
		r.facts.closed.Store(true)
	}
}

func (s *factSession) matches(ctx context.Context, req Request) bool {
	if s.closed.Load() || ctx == nil || s.bound == nil {
		return false
	}
	p, present := PrincipalFromContext(ctx)
	bound, boundPresent := PrincipalFromContext(s.bound)
	return p == bound && present == boundPresent &&
		ctx.Value(principalBindingKey{}) == s.bound.Value(principalBindingKey{}) &&
		contextx.HasSameTenantBinding(ctx, s.bound) &&
		req.Resource == s.intent.Resource && req.Action == s.intent.Action && req.Permission == s.intent.Permission &&
		reflect.DeepEqual(req.IDs, s.intent.IDs) && reflect.DeepEqual(req.Targets, s.intent.Targets)
}

func (s *factSession) get(ctx context.Context, req Request, source IFactSource) (any, error) {
	if s.closed.Load() {
		return nil, errors.NewCode(errors.FailedPrecondition, "authorization fact session is closed")
	}
	if !s.matches(ctx, req) {
		return nil, errors.NewCode(errors.InvalidInput, "fact request identity or intent changed")
	}
	result, registered := s.results[source]
	if !registered {
		return nil, errors.NewCode(errors.InvalidInput, "fact source is not registered in the authorization plan").WithContext("fact", source.FactName())
	}
	result.once.Do(func() {
		defer result.ready.Store(true)
		intent, err := s.intent.Copy()
		if err != nil {
			result.err = err
			return
		}
		// 关联提供方使用构造时固定的父投影；普通提供方读取独立意图。
		intent.facts = nil
		if dependent, ok := source.(interface{ factDependencies() []IFactSource }); ok && len(dependent.factDependencies()) > 0 {
			intent.facts = s
		}
		value, err := source.loadFact(ctx, intent)
		if err != nil {
			result.err = err
			return
		}
		result.value, result.err = copyFact(value)
	})
	if result.err != nil {
		return nil, result.err
	}
	return copyFact(result.value)
}

func copyFact(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return meta.Clone(value)
}

func collectFactSources(values ...any) []IFactSource {
	var sources []IFactSource
	seen := map[IFactSource]bool{}
	var add func(IFactSource)
	add = func(source IFactSource) {
		if depcheck.IsNil(source) || seen[source] {
			if depcheck.IsNil(source) {
				sources = append(sources, source)
			}
			return
		}
		seen[source] = true
		sources = append(sources, source)
		if dependent, ok := source.(interface{ factDependencies() []IFactSource }); ok {
			for _, parent := range dependent.factDependencies() {
				add(parent)
			}
		}
	}
	for _, value := range values {
		if consumer, ok := value.(IFactConsumer); ok && !depcheck.IsNil(consumer) {
			for _, source := range consumer.FactSources() {
				add(source)
			}
		}
	}
	return sources
}

// ReuseFacts 为多资源执行绑定前序请求中已加载的共同投影。
// 要求相同主体、租户及目标意图，以及两份计划注册的同一提供方实例。
// req 必须是尚未绑定会话的当前资源意图。
// 当前资源仍独立授权和治理；未读取的投影保持按需解析。
func (a *Authorizer) ReuseFacts(ctx context.Context, req, previous Request) (Request, error) {
	if req.facts != nil || previous.facts == nil || !previous.facts.matches(ctx, previous) || !reflect.DeepEqual(req.IDs, previous.IDs) || !reflect.DeepEqual(req.Targets, previous.Targets) {
		return Request{}, errors.NewCode(errors.InvalidInput, "fact reuse requires the same managed identity and target intent")
	}
	prepared, err := a.prepareRequest(ctx, req)
	if err != nil {
		return Request{}, err
	}
	if prepared.facts == nil {
		return prepared, nil
	}
	for source := range prepared.facts.results {
		if result, ok := previous.facts.results[source]; ok && result.ready.Load() {
			prepared.facts.results[source] = result
		}
	}
	return prepared, nil
}

// WithFacts 为函数或扩展策略登记可能使用的投影，读取仍发生在 Get 调用处。
func WithFacts(policy IPolicy, sources ...IFactSource) (IPolicy, error) {
	if depcheck.IsNil(policy) {
		return nil, errors.NewCode(errors.InvalidInput, "fact policy cannot be nil")
	}
	return &factPolicy{IPolicy: policy, sources: append(collectFactSources(policy), sources...)}, nil
}

type factPolicy struct {
	IPolicy
	sources []IFactSource
}

func (p *factPolicy) FactSources() []IFactSource { return append([]IFactSource(nil), p.sources...) }
func (p *factPolicy) UsesAllowAuthorizer() bool {
	marker, ok := p.IPolicy.(interface{ UsesAllowAuthorizer() bool })
	return ok && marker.UsesAllowAuthorizer()
}

func (a *Authorizer) prepareRequest(ctx context.Context, req Request) (Request, error) {
	frozen, err := req.Copy()
	if err != nil {
		return Request{}, err
	}
	if a.plan != nil && frozen.Permission == "" {
		frozen.Permission, _ = a.plan.Descriptor.PermissionFor(frozen.Action)
	}
	if len(a.factSources) == 0 {
		frozen.facts = nil
		return frozen, nil
	}
	if frozen.facts != nil {
		if frozen.facts.owner != a || !frozen.facts.matches(ctx, frozen) {
			return Request{}, errors.NewCode(errors.InvalidInput, "prepared facts do not match this execution")
		}
		return frozen, nil
	}
	intent, err := frozen.Copy()
	if err != nil {
		return Request{}, err
	}
	results := make(map[IFactSource]*factResult, len(a.factSources))
	for _, source := range a.factSources {
		results[source] = &factResult{}
	}
	frozen.facts = &factSession{owner: a, bound: ctx, intent: intent, results: results}
	return frozen, nil
}

// PrepareFacts 通过已注册提供方预读可信投影，供业务已需读取的路径复用。
// 返回的请求仅能在同一授权器、身份绑定和意图下复用；最终写入仍需条件与版本验证。
// 普通 Authorize 消费后关闭会话；需显式跨资源复用时使用 AuthorizeRequest，并在执行结束时 CloseFacts。
func (a *Authorizer) PrepareFacts(ctx context.Context, req Request, sources ...IFactSource) (Request, error) {
	prepared, err := a.prepareRequest(ctx, req)
	if err != nil {
		return Request{}, err
	}
	for _, source := range sources {
		if depcheck.IsNil(source) || prepared.facts == nil {
			return Request{}, errors.NewCode(errors.InvalidInput, "prepared fact must be registered")
		}
		if _, err := prepared.facts.get(ctx, prepared, source); err != nil {
			return Request{}, err
		}
	}
	return prepared, nil
}
