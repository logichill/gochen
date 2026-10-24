package governance

import (
	"context"

	"gochen/auth/scoped"
	"gochen/errors"
)

// --- Snapshot 层：固定策略版本 ---

type snapshotAuthorizer struct {
	inner scoped.IAuthorizer
	cfg   *config
}

func (a *snapshotAuthorizer) Authorize(ctx context.Context, action string, targets ...any) (scoped.Decision, error) {
	snapshot, err := a.cfg.snapshot.ResolveSnapshot(ctx)
	if err != nil {
		if !a.cfg.lenientSnapshot {
			// Fail-Closed：拿不到策略快照说明无法保证判定依据的一致性。
			return scoped.Deny("snapshot_unavailable"), errors.Wrap(err, errors.Forbidden,
				"policy snapshot is unavailable; denying request")
		}
		// 宽松模式：继续判定，但不带快照版本。
		return a.inner.Authorize(ctx, action, targets...)
	}
	if snapshot.Key == "" {
		if diag, ok := a.cfg.snapshot.(snapshotDiagnostics); ok {
			snapshot.Key = diag.SnapshotKey()
		}
	}
	recordSnapshotCapture(ctx, snapshot)
	decision, err := a.inner.Authorize(ctx, action, targets...)
	if err != nil {
		return decision, err
	}
	decision.SnapshotVersion = NormalizeSnapshot(snapshot).Version
	return decision, nil
}

func (a *snapshotAuthorizer) Require(ctx context.Context, action string, targets ...any) error {
	return requireVia(ctx, a, action, targets...)
}
