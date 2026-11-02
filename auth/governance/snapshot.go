package governance

import (
	"context"
	"strings"
	"time"

	"gochen/errors"
)

// PolicySnapshot 表达一次策略快照。
type PolicySnapshot struct {
	Key       string
	Version   string
	Timestamp time.Time
	Metadata  map[string]any

	// cacheHit 记录本次解析是否命中 TTL 缓存（诊断用，不参与序列化）。
	// 由 storeBackedSnapshot.ResolveSnapshot 填充，随单次返回值传播，
	// 使并发请求的诊断信息互不干扰。
	cacheHit bool
}

// SnapshotCacheHit 返回本次解析是否直接命中 TTL 缓存。
//
// 实现 snapshotResultDiagnostics（见 snapshot_store.go），供审计层读取。
func (s PolicySnapshot) SnapshotCacheHit() bool { return s.cacheHit }

// WithSnapshotCacheHit 返回一个标记了缓存命中诊断的副本。
func (s PolicySnapshot) WithSnapshotCacheHit(hit bool) PolicySnapshot {
	s.cacheHit = hit
	return s
}

// snapshotCaptureKey 是请求上下文注入快照诊断槽位的私有键。
type snapshotCaptureKey struct{}

// snapshotCapture 承载单次请求的快照解析诊断，消除共享实例级状态在并发下的竞争。
type snapshotCapture struct {
	resolved bool
	snapshot PolicySnapshot
}

func withSnapshotCapture(ctx context.Context, capture *snapshotCapture) context.Context {
	if ctx == nil || capture == nil {
		return ctx
	}
	return context.WithValue(ctx, snapshotCaptureKey{}, capture)
}

func recordSnapshotCapture(ctx context.Context, snapshot PolicySnapshot) {
	if ctx == nil {
		return
	}
	if capture, ok := ctx.Value(snapshotCaptureKey{}).(*snapshotCapture); ok && capture != nil {
		capture.resolved = true
		capture.snapshot = snapshot
	}
}

// IPolicySnapshot 解析治理留痕使用的策略快照。
//
// 策略实现负责消费相同版本；请求/事务/批处理内的固定范围由接入方建立。
type IPolicySnapshot interface {
	ResolveSnapshot(ctx context.Context) (PolicySnapshot, error)
}

// PolicySnapshotFunc 允许用函数直接实现 IPolicySnapshot。
type PolicySnapshotFunc func(ctx context.Context) (PolicySnapshot, error)

// ResolveSnapshot 解析当前策略快照。
func (f PolicySnapshotFunc) ResolveSnapshot(ctx context.Context) (PolicySnapshot, error) {
	if f == nil {
		return PolicySnapshot{}, errors.NewCode(
			errors.InvalidInput,
			"policy snapshot function cannot be nil; this indicates a wiring bug",
		)
	}
	return f(ctx)
}

// SnapshotVersionDerivedMetadataKey 标记快照版本是框架推导而非策略存储提供。
const SnapshotVersionDerivedMetadataKey = "snapshot_version_derived"

// NormalizeSnapshot 规范化快照的空白字段。
func NormalizeSnapshot(snapshot PolicySnapshot) PolicySnapshot {
	snapshot.Key = strings.TrimSpace(snapshot.Key)
	snapshot.Version = strings.TrimSpace(snapshot.Version)
	return snapshot
}
