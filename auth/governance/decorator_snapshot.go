package governance

import (
	"context"

	"gochen/auth"
	"gochen/errors"
)

// --- Snapshot 层：解析并记录策略版本 ---

type snapshotAuthorizer struct {
	inner auth.IAuthorizer
	cfg   *config
}

func (a *snapshotAuthorizer) Authorize(ctx context.Context, req auth.Request) (auth.Decision, error) {
	snapshot, err := a.cfg.snapshot.ResolveSnapshot(ctx)
	if err != nil {
		if !a.cfg.lenientSnapshot {
			// 默认要求快照解析成功，失败时阻断底层判定。
			return auth.Decision{Reason: "snapshot_unavailable"}, errors.Wrap(err, errors.Forbidden,
				"policy snapshot is unavailable; denying request")
		}
		// 宽松模式：继续判定，但不带快照版本。
		return a.inner.Authorize(ctx, req)
	}
	if snapshot.Key == "" {
		if diag, ok := a.cfg.snapshot.(snapshotDiagnostics); ok {
			snapshot.Key = diag.SnapshotKey()
		}
	}
	recordSnapshotCapture(ctx, snapshot)
	decision, err := a.inner.Authorize(ctx, req)
	if err != nil {
		return decision, err
	}
	decision.SnapshotVersion = NormalizeSnapshot(snapshot).Version
	return decision, nil
}

func (a *snapshotAuthorizer) Vocabulary() auth.Vocabulary { return a.inner.Vocabulary().Copy() }

func (a *snapshotAuthorizer) UsesAllowAuthorizer() bool { return auth.IsAllow(a.inner) }
