package scoped

import (
	"strings"
)

// Effect 表示授权判定结果。
type Effect string

const (
	// EffectDeny 表示拒绝。
	//
	// 注意 EffectDeny 是 Effect 的零值语义锚点：未显式置为 allow 的决策一律视为拒绝。
	EffectDeny Effect = "deny"
	// EffectAllow 表示允许。
	EffectAllow Effect = "allow"
)

// 常用拒绝原因码。
const (
	ReasonPrincipalMissing = "principal_missing"
	ReasonActionDenied     = "action_denied"
	ReasonOutOfScope       = "out_of_scope"
	ReasonNoResource       = "no_resource"
)

// Decision 表达一次授权判定的结构化结果。
type Decision struct {
	ID                  string
	Effect              Effect
	ReasonCode          string
	SnapshotVersion     string
	MatchedRules        []string
	AuthorizedResources []Resource
}

// IsAllowed 判断决策是否允许，且确实授权了资源。
//
// allow 但没有任何授权资源视为不允许——避免"空 allow"被当成通行证。
// 该判据只适用于**已知具体目标**的判定；没有目标可传的场景见 IsActionAllowed。
func (d Decision) IsAllowed() bool {
	return d.Effect == EffectAllow && len(d.AuthorizedResources) > 0
}

// IsActionAllowed 判断决策是否在**动作层面**允许。
//
// 用于没有具体资源目标的判定——典型是列表读：调用方问的是"能不能列这类资源"，
// 可见范围随后由 DataScope 决定，PDP 无从也无需回显一份资源清单。
// 此时 IsAllowed 的"必须有授权资源"要求无法满足，评估器只能伪造一个占位资源
// 才能放行，那恰恰把"空 allow 通行证"这个本要防的东西请了回来。
//
// 铁律：本方法**不得**用于写路径或已知目标的读路径——那些场景必须用 IsAllowed，
// 由授权资源清单明确界定作用对象。
func (d Decision) IsActionAllowed() bool { return d.Effect == EffectAllow }

// AllowedFor 判断决策是否放行给定的目标数量。
//
// 有具体目标时要求 IsAllowed（必须明确授权资源）；
// 无目标（动作级判定）时使用 IsActionAllowed，避免动作级授权被空资源清单误杀。
func (d Decision) AllowedFor(targetCount int) bool {
	if targetCount > 0 {
		return d.IsAllowed()
	}
	return d.IsActionAllowed()
}

// Allow 创建一个 allow 判定。
func Allow(resources ...Resource) Decision {
	normalized := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		normalized = append(normalized, resource.Normalize())
	}
	return Decision{Effect: EffectAllow, AuthorizedResources: normalized}
}

// Deny 创建一个带原因码的 deny 判定。
func Deny(reasonCode string) Decision {
	return Decision{Effect: EffectDeny, ReasonCode: strings.TrimSpace(reasonCode)}
}

// WriteConstraint 把 allow 决策投影为显式写入约束。
//
// 非 allow 或无授权资源时返回空约束——下游读到空约束必须拒绝。
func (d Decision) WriteConstraint() WriteConstraint {
	constraint := WriteConstraint{}
	if !d.IsAllowed() {
		return constraint
	}
	constraint.Resources = make([]ResourceConstraint, 0, len(d.AuthorizedResources))
	for _, resource := range d.AuthorizedResources {
		resource = resource.Normalize()
		constraint.Resources = append(constraint.Resources, ResourceConstraint{
			Kind:           resource.Kind,
			ResourceID:     resource.ID,
			ManagedScopeID: resource.ManagedScopeID,
			GlobalScope:    resource.GlobalScope,
			TenantID:       resource.TenantID,
			Revision:       resource.Revision,
		})
	}
	return constraint
}

// DataScope 把 allow 决策授权的资源投影为数据范围。
func (d Decision) DataScope() DataScope {
	if !d.IsAllowed() {
		return DenyAll()
	}
	return d.WriteConstraint().DataScope()
}
