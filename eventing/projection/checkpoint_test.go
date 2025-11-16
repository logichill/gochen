package projection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewCheckpoint 测试创建检查点
func TestNewCheckpoint(t *testing.T) {
	now := time.Now()
	checkpoint := NewCheckpoint("test-projection", 100, "event-123", now)

	assert.Equal(t, "test-projection", checkpoint.ProjectionName)
	assert.Equal(t, int64(100), checkpoint.Position)
	assert.Equal(t, "event-123", checkpoint.LastEventID)
	assert.Equal(t, now, checkpoint.LastEventTime)
	assert.False(t, checkpoint.UpdatedAt.IsZero())
}

// TestCheckpoint_IsValid 测试检查点验证
func TestCheckpoint_IsValid(t *testing.T) {
	tests := []struct {
		name       string
		checkpoint *Checkpoint
		want       bool
	}{
		{
			name: "valid checkpoint",
			checkpoint: &Checkpoint{
				ProjectionName: "test",
				Position:       10,
			},
			want: true,
		},
		{
			name: "empty name",
			checkpoint: &Checkpoint{
				ProjectionName: "",
				Position:       10,
			},
			want: false,
		},
		{
			name: "negative position",
			checkpoint: &Checkpoint{
				ProjectionName: "test",
				Position:       -1,
			},
			want: false,
		},
		{
			name: "zero position is valid",
			checkpoint: &Checkpoint{
				ProjectionName: "test",
				Position:       0,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.checkpoint.IsValid())
		})
	}
}

// TestCheckpoint_Clone 测试克隆检查点
func TestCheckpoint_Clone(t *testing.T) {
	original := NewCheckpoint("test", 100, "event-123", time.Now())
	cloned := original.Clone()

	assert.Equal(t, original.ProjectionName, cloned.ProjectionName)
	assert.Equal(t, original.Position, cloned.Position)
	assert.Equal(t, original.LastEventID, cloned.LastEventID)
	assert.Equal(t, original.LastEventTime, cloned.LastEventTime)
	assert.Equal(t, original.UpdatedAt, cloned.UpdatedAt)

	// 修改克隆不影响原始
	cloned.Position = 200
	assert.Equal(t, int64(100), original.Position)
	assert.Equal(t, int64(200), cloned.Position)
}

// TestCheckpoint_Update 测试更新检查点
func TestCheckpoint_Update(t *testing.T) {
	checkpoint := NewCheckpoint("test", 100, "event-123", time.Now())
	time.Sleep(10 * time.Millisecond) // 确保时间不同

	newTime := time.Now()
	checkpoint.Update(200, "event-456", newTime)

	assert.Equal(t, int64(200), checkpoint.Position)
	assert.Equal(t, "event-456", checkpoint.LastEventID)
	assert.Equal(t, newTime, checkpoint.LastEventTime)
	assert.True(t, checkpoint.UpdatedAt.After(newTime) || checkpoint.UpdatedAt.Equal(newTime))
}

// BenchmarkCheckpoint_Create 性能测试 - 创建检查点
func BenchmarkCheckpoint_Create(b *testing.B) {
	now := time.Now()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewCheckpoint("test", int64(i), "event-123", now)
	}
}

// BenchmarkCheckpoint_Clone 性能测试 - 克隆检查点
func BenchmarkCheckpoint_Clone(b *testing.B) {
	checkpoint := NewCheckpoint("test", 100, "event-123", time.Now())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = checkpoint.Clone()
	}
}
