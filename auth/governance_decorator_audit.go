package auth

import (
	"context"
	"fmt"

	"gochen/errors"
)

// --- Audit 层：决策留痕 ---

type auditAuthorizer struct {
	inner IAuthorizer
	cfg   *config
}

func (a *auditAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	start := a.cfg.clock.Now()
	var capture snapshotCapture
	auditCtx := withSnapshotCapture(ctx, &capture)
	decision, err := a.inner.Authorize(auditCtx, req)
	if decision.ID == "" {
		id, genErr := a.cfg.idGenerator.Next()
		switch {
		case genErr != nil && a.cfg.strictAudit:
			// 严格模式与审计写入失败同口径：留痕要素缺失即拒绝。
			return Decision{Reason: "audit_unavailable"}, errors.Wrap(genErr, errors.Forbidden,
				"failed to assign decision ID; denying request")
		case genErr == nil:
			decision.ID = id
		default:
			// 非严格模式：生成器故障时使用内联降级 ID 兜底，保证审计条目
			// 主键始终非空，避免空 ID 入库冲突导致审计链路中断。
			decision.ID = fallbackDecisionID()
		}
	}

	entry := AuthzLogEntry{
		ID:              decision.ID,
		Type:            AuthzLogEntryTypeDecision,
		Timestamp:       start,
		DecisionID:      decision.ID,
		Action:          req.Permission,
		Effect:          decisionEffect(decision),
		ReasonCode:      decision.Reason,
		SnapshotVersion: decision.SnapshotVersion,
		LatencyMs:       a.cfg.clock.Now().Sub(start).Milliseconds(),
		Resources:       LoggedResourcesOf(req),
		MatchedRules:    decision.MatchedRules,
	}
	if decision.SnapshotVersion != "" && capture.resolved {
		entry.CacheHit = capture.snapshot.SnapshotCacheHit()
		entry.SnapshotKey = capture.snapshot.Key
	}
	// Wrap 在交付前统一脱敏：任何 IAuthzAuditStore（包括自定义实现）收到的条目
	// 都必须已剔除横向关联标识（TenantID / OwnerID），不依赖各家 store 自觉。
	// 需要保留完整字段的合规场景经 WithResourceLogSanitizer 注入；
	// store 侧可保留同构脱敏作为自身对直连调用方的输入边界（幂等）。
	entry = SanitizeAuthzLogEntry(entry)
	entry.Resources = SanitizeLoggedResources(ctx, entry.Resources)
	if auditErr := a.saveAuditEntrySafely(ctx, entry); auditErr != nil {
		if a.cfg.strictAudit {
			// 严格模式：留痕失败即拒绝，保证“无审计不放行”。
			return Decision{Reason: "audit_unavailable"}, errors.Wrap(auditErr, errors.Forbidden,
				"authorization audit store is unavailable; denying request")
		}
		// 默认 fail-open：审计不可用不阻塞授权。
	}
	return decision, err
}

// saveAuditEntrySafely 保存审计条目；非严格模式下审计适配器的 panic 与错误一样
// 属于“审计不可用”，必须按文档化的 fail-open 契约降级为错误返回，而不得让
// panic 逃逸阻断主业务的鉴权放行。
func (a *auditAuthorizer) saveAuditEntrySafely(ctx context.Context, entry AuthzLogEntry) (auditErr error) {
	if !a.cfg.strictAudit {
		defer func() {
			if r := recover(); r != nil {
				auditErr = errors.NewCode(errors.Internal, "authorization audit store panicked").
					WithContext("panic", fmt.Sprint(r))
			}
		}()
	}
	return a.cfg.auditStore.SaveAuthzLogEntry(ctx, entry)
}

func (a *auditAuthorizer) Vocabulary() Vocabulary { return a.inner.Vocabulary().Copy() }

func (a *auditAuthorizer) UsesAllowAuthorizer() bool { return IsAllow(a.inner) }
