// Package auth 提供授权端口与共享值对象。
//
// 业务侧只依赖本包的 IAuthorizer；具体授权模式由 action.RBAC、scoped.New
// 或外部引擎实现。身份仍在 ctx 中传递。
package auth

import (
	"context"
	"reflect"
	"strings"

	"gochen/domain/spec"
	"gochen/errors"
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
}

// Decision 是一次判定结果；零值即拒绝。
type Decision struct {
	Allowed bool
	Reason  string
	Spec    spec.Spec
	Assign  []Assignment
}

// Assignment 是一条服务端权威赋值，只能针对 RoleScope、RoleOwner。
type Assignment struct {
	Role  spec.Role
	Value any
}

// Validate 校验赋值针对的角色。
func (a Assignment) Validate() error {
	switch a.Role {
	case spec.RoleScope, spec.RoleOwner:
		return nil
	default:
		return errors.NewCode(errors.InvalidInput, "assignment role is not allowed").
			WithContext("role", string(a.Role))
	}
}

// Vocabulary 声明判定结果可能引用的角色、字段与命名条件。
type Vocabulary struct {
	Roles  []spec.Role
	Fields []string
	Named  []string
}

// Kind 描述动作的流程形状。
type Kind string

const (
	KindRead   Kind = "read"
	KindList   Kind = "list"
	KindCreate Kind = "create"
	KindUpdate Kind = "update"
	KindDelete Kind = "delete"
)

// Action 是 Descriptor 中的一个动作声明。
type Action struct {
	Name       string
	Permission string
	Kind       Kind
	Label      string
	Risk       string
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

// IsAllow 报告授权器是否为 Allow() 返回的一律放行实现。
func IsAllow(a IAuthorizer) bool {
	_, ok := a.(allowAuthorizer)
	return ok
}

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
		if a == nil {
			return nil, errors.NewCode(errors.InvalidInput, "auth.All authorizer cannot be nil")
		}
		out.vocabulary = mergeVocabulary(out.vocabulary, a.Vocabulary())
	}
	return out, nil
}

func (a *allAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	specs := make([]spec.Spec, 0, len(a.authorizers))
	assignments := make(map[spec.Role]any)
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
			if existing, ok := assignments[assignment.Role]; ok {
				if !reflect.DeepEqual(existing, assignment.Value) {
					return Decision{Allowed: false, Reason: "assignment_conflict"}, nil
				}
				continue
			}
			assignments[assignment.Role] = assignment.Value
		}
	}
	out := Decision{Allowed: true, Spec: spec.And(specs...)}
	for role, value := range assignments {
		out.Assign = append(out.Assign, Assignment{Role: role, Value: value})
	}
	return out, nil
}

func (a *allAuthorizer) Vocabulary() Vocabulary { return a.vocabulary }

func mergeVocabulary(a, b Vocabulary) Vocabulary {
	a.Roles = appendUniqueRoles(a.Roles, b.Roles...)
	a.Fields = appendUnique(a.Fields, b.Fields...)
	a.Named = appendUnique(a.Named, b.Named...)
	return a
}

func appendUniqueRoles(dst []spec.Role, values ...spec.Role) []spec.Role {
	seen := make(map[spec.Role]struct{}, len(dst)+len(values))
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

func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
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
