package auth

import (
	"strings"

	"gochen/errors"
	"gochen/ident"
)

// Effect 表示授权判定的结果。
type Effect string

const (
	// EffectAllow 表示允许。
	EffectAllow Effect = "allow"
	// EffectDeny 表示拒绝。
	EffectDeny Effect = "deny"
)

const (
	// ReasonCodePrincipalMissing 表示缺少主体信息。
	ReasonCodePrincipalMissing = "principal_missing"
)

// AuthzDecision 表达一次授权判定的结构化结果。
type AuthzDecision struct {
	ID                  string
	Effect              Effect
	ReasonCode          string
	SnapshotVersion     string
	Consistency         ConsistencyMode
	MatchedRules        []string
	AuthorizedResources []Resource
}

// AllowDecision 创建一个 allow 判定。
func AllowDecision(resources ...Resource) AuthzDecision {
	return AuthzDecision{
		ID:                  nextDecisionID(),
		Effect:              EffectAllow,
		Consistency:         ConsistencyModeStrong,
		AuthorizedResources: cloneResources(resources),
	}
}

// DenyDecision 创建一个 deny 判定。
func DenyDecision(reasonCode string, resources ...Resource) AuthzDecision {
	return AuthzDecision{
		ID:                  nextDecisionID(),
		Effect:              EffectDeny,
		ReasonCode:          strings.TrimSpace(reasonCode),
		Consistency:         ConsistencyModeStrong,
		AuthorizedResources: cloneResources(resources),
	}
}

// RequireAllow 要求当前判定必须为 allow，否则映射为统一错误语义。
func (d AuthzDecision) RequireAllow() error {
	decision := normalizeDecision(d)
	switch decision.Effect {
	case EffectAllow:
		return nil
	case EffectDeny:
		code := errors.Forbidden
		message := "permission denied"
		if decision.ReasonCode == ReasonCodePrincipalMissing {
			code = errors.Unauthorized
			message = "principal is required"
		}
		err := errors.NewCode(code, message)
		if decision.ID != "" {
			err = err.WithContext("decision_id", decision.ID)
		}
		if decision.ReasonCode != "" {
			err = err.WithContext("reason_code", decision.ReasonCode)
		}
		if decision.SnapshotVersion != "" {
			err = err.WithContext("snapshot_version", decision.SnapshotVersion)
		}
		if decision.Consistency != ConsistencyModeUnspecified {
			err = err.WithContext("consistency", string(decision.Consistency))
		}
		if len(decision.MatchedRules) > 0 {
			err = err.WithContext("matched_rules", append([]string(nil), decision.MatchedRules...))
		}
		return err
	default:
		return errors.NewCode(errors.ServiceUnavailable, "authorization decision is incomplete")
	}
}

func normalizeDecision(decision AuthzDecision) AuthzDecision {
	decision.ID = strings.TrimSpace(decision.ID)
	decision.ReasonCode = strings.TrimSpace(decision.ReasonCode)
	decision.SnapshotVersion = strings.TrimSpace(decision.SnapshotVersion)
	decision.Consistency = normalizeConsistencyMode(decision.Consistency)
	if decision.Consistency == ConsistencyModeUnspecified {
		decision.Consistency = ConsistencyModeStrong
	}
	decision.MatchedRules = normalizeStrings(decision.MatchedRules)
	decision.AuthorizedResources = cloneResources(decision.AuthorizedResources)
	if decision.ID == "" {
		decision.ID = nextDecisionID()
	}
	return decision
}

func cloneResources(resources []Resource) []Resource {
	if len(resources) == 0 {
		return nil
	}
	cloned := make([]Resource, 0, len(resources))
	for _, resource := range resources {
		cloned = append(cloned, normalizeResource(resource))
	}
	return cloned
}

func nextDecisionID() string {
	generator := ident.DefaultStringGenerator()
	if generator == nil {
		return ""
	}
	id, err := generator.Next()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}
