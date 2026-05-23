package access

import (
	"context"
	"strings"
)

// ConstraintMetadata 表达与写入约束伴随传播的观测元数据。
type ConstraintMetadata struct {
	DecisionID      string
	SnapshotVersion string
	Consistency     string
}

type constraintMetadataContextKey struct{}

// WithConstraintMetadata 将约束元数据绑定到 context。
func WithConstraintMetadata(ctx context.Context, metadata ConstraintMetadata) context.Context {
	if ctx == nil {
		return nil
	}
	normalized := ConstraintMetadata{
		DecisionID:      stringsTrim(metadata.DecisionID),
		SnapshotVersion: stringsTrim(metadata.SnapshotVersion),
		Consistency:     stringsTrim(metadata.Consistency),
	}
	if normalized == (ConstraintMetadata{}) {
		return ctx
	}
	return context.WithValue(ctx, constraintMetadataContextKey{}, normalized)
}

// ConstraintMetadataFromContext 从 context 读取约束元数据。
func ConstraintMetadataFromContext(ctx context.Context) (ConstraintMetadata, bool) {
	if ctx == nil {
		return ConstraintMetadata{}, false
	}
	metadata, ok := ctx.Value(constraintMetadataContextKey{}).(ConstraintMetadata)
	if !ok {
		return ConstraintMetadata{}, false
	}
	metadata = ConstraintMetadata{
		DecisionID:      stringsTrim(metadata.DecisionID),
		SnapshotVersion: stringsTrim(metadata.SnapshotVersion),
		Consistency:     stringsTrim(metadata.Consistency),
	}
	return metadata, metadata != (ConstraintMetadata{})
}

func stringsTrim(v string) string {
	return strings.TrimSpace(v)
}
