package projection

import (
	"gochen/contextx"
	"gochen/eventing"
	"gochen/logging"
	"time"
)

// ProjectionConfig 投影配置。
//
// 用于配置投影的错误处理和重试策略。
type ProjectionConfig struct {
	// MaxRetries 表示单个事件在失败后的最大重试次数。
	//
	// 语义说明：
	//   - 0 表示不重试（仅执行一次 Handle）；
	//   - >0 表示在首次失败后最多再重试 MaxRetries 次（总尝试次数 <= 1+MaxRetries）；
	//   - <0 视为配置错误，将按 0 处理（不重试）。
	MaxRetries int

	// RetryBackoff 重试退避时间
	RetryBackoff time.Duration

	// DeadLetterFunc 死信处理函数（重试失败后调用）。
	// 可用于记录日志、发送告警或将事件发送到死信队列。
	//
	// 注意：ProjectionManager 泛型化后，死信回调中的事件类型也随 ID 一起泛型化。
	DeadLetterFunc func(err error, event eventing.IEvent, projection string)

	// CheckpointSaveInterval 检查点保存间隔（时间维度）
	// 达到此时间间隔后保存检查点。
	// 默认值 5s，设置为 0 表示禁用时间维度策略。
	CheckpointSaveInterval time.Duration

	// CheckpointSaveCount 检查点保存间隔（事件数维度）
	// 每处理 N 个事件后保存检查点。
	// 默认值 100，设置为 0 表示禁用事件数维度策略。
	// 注意：如果两个维度都设置为 0，则每个事件都会保存检查点（不推荐）。
	CheckpointSaveCount int
}

func defaultDeadLetterFunc() func(err error, event eventing.IEvent, projection string) {
	return func(err error, event eventing.IEvent, projection string) {
		ctx := contextx.Background()
		if event == nil {
			projectionLogger().Error(ctx, "event processing failed after max retries", logging.Error(err),
				logging.String("projection", projection),
			)
			return
		}
		projectionLogger().Error(ctx, "event processing failed after max retries", logging.Error(err),
			logging.String("projection", projection),
			logging.String("event_id", event.GetID()),
			logging.String("event_type", event.GetType()),
		)
	}
}

// normalizeProjectionConfig 规范化投影配置。
func normalizeProjectionConfig(config *ProjectionConfig) *ProjectionConfig {
	if config == nil {
		config = &ProjectionConfig{
			MaxRetries:             3,
			RetryBackoff:           1 * time.Second,
			CheckpointSaveInterval: 5 * time.Second,
			CheckpointSaveCount:    100,
			DeadLetterFunc:         defaultDeadLetterFunc(),
		}
	}

	out := *config

	if out.MaxRetries < 0 {
		out.MaxRetries = 0
	}
	if out.RetryBackoff < 0 {
		out.RetryBackoff = 0
	}
	if out.DeadLetterFunc == nil {
		out.DeadLetterFunc = defaultDeadLetterFunc()
	}

	// CheckpointSaveInterval/Count 的 0/0 组合语义为“每个事件都保存检查点”（不推荐），但负数通常是误配置；
	// 若检测到负数误配置且最终落入 0/0，则回退到默认值，避免引入意外的高频写入。
	invalidCheckpointConfig := false
	if out.CheckpointSaveInterval < 0 {
		out.CheckpointSaveInterval = 0
		invalidCheckpointConfig = true
	}
	if out.CheckpointSaveCount < 0 {
		out.CheckpointSaveCount = 0
		invalidCheckpointConfig = true
	}
	if invalidCheckpointConfig && out.CheckpointSaveInterval <= 0 && out.CheckpointSaveCount <= 0 {
		out.CheckpointSaveInterval = 5 * time.Second
		out.CheckpointSaveCount = 100
	}

	return &out
}

// PresetProjectionConfigs 提供投影配置预设，用于按场景快速选择 checkpoint 保存频率。
//
// 说明：
// - LowLatency：更频繁保存 checkpoint，恢复/追赶更“细”，但会增加 DB 写入。
// - Balanced：默认平衡策略。
// - HighThroughput：更少保存 checkpoint，降低 DB 写入，适合追赶阶段高吞吐。
type PresetProjectionConfigs struct{}

func (PresetProjectionConfigs) LowLatency() *ProjectionConfig {
	return normalizeProjectionConfig(&ProjectionConfig{
		MaxRetries:             3,
		RetryBackoff:           1 * time.Second,
		CheckpointSaveInterval: 1 * time.Second,
		CheckpointSaveCount:    50,
		DeadLetterFunc:         defaultDeadLetterFunc(),
	})
}

func (PresetProjectionConfigs) Balanced() *ProjectionConfig {
	return DefaultProjectionConfig()
}

func (PresetProjectionConfigs) HighThroughput() *ProjectionConfig {
	return normalizeProjectionConfig(&ProjectionConfig{
		MaxRetries:             3,
		RetryBackoff:           1 * time.Second,
		CheckpointSaveInterval: 15 * time.Second,
		CheckpointSaveCount:    1000,
		DeadLetterFunc:         defaultDeadLetterFunc(),
	})
}

// ProjectionConfigPresets 投影配置预设入口。
var ProjectionConfigPresets = PresetProjectionConfigs{}

// DefaultProjectionConfig 默认投影配置。
func DefaultProjectionConfig() *ProjectionConfig {
	return normalizeProjectionConfig(nil)
}
