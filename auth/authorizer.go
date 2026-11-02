// Package auth 提供授权端口与共享值对象。
//
// 业务侧只依赖本包的 IAuthorizer；具体授权模式由 action.RBAC、scoped.New
// 或外部引擎实现。身份仍在 ctx 中传递。
package auth

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// IAuthorizer 是唯一的授权端口：Application 在每个用例入口调用一次
// Authorize，构造时读取一次 Vocabulary。
type IAuthorizer interface {
	Authorize(ctx context.Context, req Request) (Decision, error)
	Vocabulary() Vocabulary
}

// Request 描述一次授权请求；主体与租户从 ctx 读取。
type Request struct {
	Resource   string
	Action     string
	Permission string
	IDs        []string
	Targets    []Assignment
}

// Decision 是一次判定结果；零值即拒绝。
type Decision struct {
	ID              string
	SnapshotVersion string
	MatchedRules    []string
	Allowed         bool
	Reason          string
	Spec            spec.Spec
	Assign          []Assignment
}

// Assignment 是针对项目维度的服务端权威赋值。
type Assignment struct {
	Dimension meta.Dimension `json:"dimension"`
	Value     any            `json:"value"`
}

// Validate 校验赋值针对的角色。
func (a Assignment) Validate() error {
	_, err := meta.ParseDimension(string(a.Dimension))
	return err
}

// Vocabulary 声明判定结果可能引用的字段与可赋值维度。
type Vocabulary struct {
	Refs       []meta.Ref
	Assignable []meta.Dimension
}

// Kind 描述动作的流程形状。
type Kind string

const (
	KindRead   Kind = "read"
	KindList   Kind = "list"
	KindCreate Kind = "create"
	KindUpdate Kind = "update"
	KindDelete Kind = "delete"
	// KindTransfer 通过独立端口执行维度迁移。
	KindTransfer Kind = "transfer"
)

// Action 是 Descriptor 中的一个动作声明。
type Action struct {
	Name       string
	Permission string
	Kind       Kind
	Label      string
	Risk       string
	// ReceiveAction 是迁移动作的接收授权动作；接收动作本身保持此字段为空。
	ReceiveAction string
	// Dimensions 限定该迁移动作获准写入的维度。
	Dimensions []meta.Dimension
}

// Descriptor 声明一个资源的动作集合，不包含 HTTP 路由。
type Descriptor struct {
	Resource string
	Actions  []Action
}

// Action 按名称查找动作。
func (d Descriptor) Action(name string) (Action, bool) {
	for _, a := range d.Actions {
		if a.Name == name {
			a.Dimensions = append([]meta.Dimension(nil), a.Dimensions...)
			return a, true
		}
	}
	return Action{}, false
}

// PermissionFor 返回动作的权限码；未显式声明时使用 <resource>:api:<action>。
func (d Descriptor) PermissionFor(name string) (string, bool) {
	a, ok := d.Action(name)
	if !ok {
		return "", false
	}
	if a.Permission != "" {
		return a.Permission, true
	}
	if d.Resource == "" || name == "" {
		return "", false
	}
	return d.Resource + ":api:" + name, true
}

// Validate 校验 Descriptor 的基本不变量。
func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.Resource) == "" {
		return errors.NewCode(errors.InvalidInput, "descriptor resource cannot be empty")
	}
	seen := make(map[string]struct{}, len(d.Actions))
	for _, a := range d.Actions {
		if strings.TrimSpace(a.Name) == "" {
			return errors.NewCode(errors.InvalidInput, "descriptor action name cannot be empty")
		}
		if _, ok := seen[a.Name]; ok {
			return errors.NewCode(errors.InvalidInput, "duplicate descriptor action").
				WithContext("action", a.Name)
		}
		seen[a.Name] = struct{}{}
	}
	return nil
}

type allowAuthorizer struct{}

// Allow 返回一律放行的授权器，规约为 spec.All()。
func Allow() IAuthorizer { return allowAuthorizer{} }

// IsAllow 报告授权器是否使用内置 Allow 策略，治理包装保留该标记。
// 该标记用于启动期清单；实际授权仍由 Authorize 决定。
func IsAllow(a IAuthorizer) bool {
	if depcheck.IsNil(a) {
		return false
	}
	provider, ok := a.(interface{ UsesAllowAuthorizer() bool })
	return ok && provider.UsesAllowAuthorizer()
}

func (allowAuthorizer) UsesAllowAuthorizer() bool { return true }

func (allowAuthorizer) Authorize(context.Context, Request) (Decision, error) {
	return Decision{Allowed: true, Spec: spec.All()}, nil
}

func (allowAuthorizer) Vocabulary() Vocabulary { return Vocabulary{} }

type allAuthorizer struct {
	authorizers []IAuthorizer
	vocabulary  Vocabulary
}

// All 组合多个授权器：全部放行才放行，规约取 And，赋值冲突即拒绝。
// 不传授权器或传入 nil 时构造失败。
func All(authorizers ...IAuthorizer) (IAuthorizer, error) {
	if len(authorizers) == 0 {
		return nil, errors.NewCode(errors.InvalidInput, "auth.All requires at least one authorizer")
	}
	out := &allAuthorizer{authorizers: append([]IAuthorizer(nil), authorizers...)}
	for _, a := range out.authorizers {
		if depcheck.IsNil(a) {
			return nil, errors.NewCode(errors.InvalidInput, "auth.All authorizer cannot be nil")
		}
		out.vocabulary = mergeVocabulary(out.vocabulary, a.Vocabulary())
	}
	return out, nil
}

func (a *allAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	specs := make([]spec.Spec, 0, len(a.authorizers))
	assignments := make(map[meta.Dimension]any)
	for _, authorizer := range a.authorizers {
		decision, err := authorizer.Authorize(ctx, req)
		if err != nil {
			return Decision{}, err
		}
		if !decision.Allowed {
			reason := decision.Reason
			if reason == "" {
				reason = "forbidden"
			}
			return Decision{Allowed: false, Reason: reason}, nil
		}
		if decision.Spec.Err() != nil {
			return Decision{}, errors.Wrap(decision.Spec.Err(), errors.Internal, "authorizer returned invalid spec")
		}
		specs = append(specs, decision.Spec)
		for _, assignment := range decision.Assign {
			if err := assignment.Validate(); err != nil {
				return Decision{}, err
			}
			if existing, ok := assignments[assignment.Dimension]; ok {
				if !reflect.DeepEqual(existing, assignment.Value) {
					return Decision{Allowed: false, Reason: "assignment_conflict"}, nil
				}
				continue
			}
			assignments[assignment.Dimension] = assignment.Value
		}
	}
	out := Decision{Allowed: true, Spec: spec.And(specs...)}
	for dimension, value := range assignments {
		out.Assign = append(out.Assign, Assignment{Dimension: dimension, Value: value})
	}
	sort.Slice(out.Assign, func(i, j int) bool { return out.Assign[i].Dimension < out.Assign[j].Dimension })
	return out, nil
}

func (a *allAuthorizer) Vocabulary() Vocabulary { return a.vocabulary.Copy() }

func (a *allAuthorizer) UsesAllowAuthorizer() bool {
	for _, inner := range a.authorizers {
		if !IsAllow(inner) {
			return false
		}
	}
	return true
}

func mergeVocabulary(a, b Vocabulary) Vocabulary {
	a.Refs = appendUniqueRefs(a.Refs, b.Refs...)
	a.Assignable = appendUniqueDimensions(a.Assignable, b.Assignable...)
	return a
}

func appendUniqueRefs(dst []meta.Ref, values ...meta.Ref) []meta.Ref {
	seen := make(map[meta.Ref]struct{}, len(dst)+len(values))
	for _, v := range dst {
		seen[v] = struct{}{}
	}
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		dst = append(dst, v)
	}
	return dst
}

func appendUniqueDimensions(dst []meta.Dimension, values ...meta.Dimension) []meta.Dimension {
	seen := make(map[meta.Dimension]struct{}, len(dst)+len(values))
	for _, v := range dst {
		seen[v] = struct{}{}
	}
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		dst = append(dst, v)
	}
	return dst
}
