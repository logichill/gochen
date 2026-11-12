package auth

import (
	"context"

	"gochen/errors"
)

// --- Snapshot 层：解析并记录策略版本 ---

type snapshotAuthorizer struct {
	inner IAuthorizer
	cfg   *config
}

func (a *snapshotAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	snapshot, err := a.cfg.snapshot.ResolveSnapshot(ctx)
	snapshot = NormalizeSnapshot(snapshot)
	if err == nil && snapshot.Version == "" {
		err = errors.NewCode(errors.InvalidInput, "policy snapshot version is required")
	}
	if err != nil {
		if !a.cfg.lenientSnapshot {
			// 默认要求快照解析成功，失败时阻断底层判定。
			return Decision{Reason: "snapshot_unavailable"}, errors.Wrap(err, errors.Forbidden,
				"policy snapshot is unavailable; denying request")
		}
		// 宽松模式：继续判定，但不带快照版本。
		return a.inner.Authorize(ctx, req)
	}
	recordSnapshotCapture(ctx, snapshot)
	decision, err := a.inner.Authorize(ctx, req)
	if err != nil {
		return decision, err
	}
	decision.SnapshotVersion = snapshot.Version
	return decision, nil
}

func (a *snapshotAuthorizer) Vocabulary() Vocabulary { return a.inner.Vocabulary().Copy() }

func (a *snapshotAuthorizer) UsesAllowAuthorizer() bool { return IsAllow(a.inner) }
