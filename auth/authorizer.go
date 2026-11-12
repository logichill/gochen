// Package auth 提供授权端口与共享值对象。
//
// 业务侧只依赖本包的 IAuthorizer；策略由声明编译或 IPolicy 扩展提供，完整执行由 New 构造。身份仍在 ctx 中传递。
package auth

import (
	"context"
	"strings"

	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/internal/depcheck"
)

// IAuthorizer 是唯一的授权端口：Application 在每个用例入口调用一次
// Authorize，构造时读取一次 Vocabulary。
// 自定义透明包装须同步转发 Plan、UsesAllowAuthorizer 和 FastDeny，
// 保持编译计划绑定、显式放行识别及提前拒绝能力，具体签名见 Authorizer。
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
	facts      *factSession
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

// Allow 返回使用完整执行内核的显式放行授权器。
func Allow() IAuthorizer { return &Authorizer{inner: &policyAuthorizer{policy: AllowPolicy()}} }

// IsAllow 返回完整授权器的显式放行标记。
func IsAllow(a IAuthorizer) bool {
	if depcheck.IsNil(a) {
		return false
	}
	marker, ok := a.(interface{ UsesAllowAuthorizer() bool })
	return ok && marker.UsesAllowAuthorizer()
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
