package saga

import (
	"encoding/json"
	"time"
)

// SagaStatus Saga 状态枚举
type SagaStatus string

const (
	// SagaStatusPending 待执行
	SagaStatusPending SagaStatus = "pending"

	// SagaStatusRunning 执行中
	SagaStatusRunning SagaStatus = "running"

	// SagaStatusCompleted 已完成
	SagaStatusCompleted SagaStatus = "completed"

	// SagaStatusFailed 已失败
	SagaStatusFailed SagaStatus = "failed"

	// SagaStatusCompensating 补偿中
	SagaStatusCompensating SagaStatus = "compensating"

	// SagaStatusCompensated 已补偿
	SagaStatusCompensated SagaStatus = "compensated"
)

// SagaState Saga 状态
//
// 记录 Saga 的执行状态，用于持久化和恢复。
//
// 特性：
//   - 记录当前步骤
//   - 记录已完成的步骤
//   - 记录失败信息
//   - 支持 JSON 序列化
type SagaState struct {
	// SagaID Saga 唯一标识
	SagaID string `json:"saga_id" db:"saga_id"`

	// SagaType Saga 类型（用于分类统计）
	SagaType string `json:"saga_type" db:"saga_type"`

	// CurrentStep 当前步骤索引（从 0 开始）
	CurrentStep int `json:"current_step" db:"current_step"`

	// Status Saga 状态
	Status SagaStatus `json:"status" db:"status"`

	// CompletedSteps 已完成的步骤名称列表
	CompletedSteps []string `json:"completed_steps" db:"completed_steps"`

	// FailedStep 失败的步骤名称
	FailedStep string `json:"failed_step,omitempty" db:"failed_step"`

	// Error 错误信息
	Error string `json:"error,omitempty" db:"error"`

	// Data 自定义数据（JSON 格式）
	Data map[string]any `json:"data,omitempty" db:"data"`

	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at" db:"created_at"`

	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// NewSagaState 创建新的 Saga 状态
//
// 参数：
//   - sagaID: Saga ID
//   - sagaType: Saga 类型
//
// 返回：
//   - *SagaState: 状态实例
func NewSagaState(sagaID, sagaType string) *SagaState {
	now := time.Now()
	return &SagaState{
		SagaID:         sagaID,
		SagaType:       sagaType,
		CurrentStep:    0,
		Status:         SagaStatusPending,
		CompletedSteps: []string{},
		Data:           make(map[string]any),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// MarkStepCompleted 标记步骤完成
//
// 参数：
//   - stepName: 步骤名称
func (s *SagaState) MarkStepCompleted(stepName string) {
	s.CompletedSteps = append(s.CompletedSteps, stepName)
	s.CurrentStep++
	s.UpdatedAt = time.Now()
}

// MarkStepFailed 标记步骤失败
//
// 参数：
//   - stepName: 步骤名称
//   - err: 错误信息
func (s *SagaState) MarkStepFailed(stepName string, err error) {
	s.FailedStep = stepName
	s.Error = err.Error()
	s.Status = SagaStatusFailed
	s.UpdatedAt = time.Now()
}

// MarkCompleted 标记 Saga 完成
func (s *SagaState) MarkCompleted() {
	s.Status = SagaStatusCompleted
	s.UpdatedAt = time.Now()
}

// MarkCompensating 标记开始补偿
func (s *SagaState) MarkCompensating() {
	s.Status = SagaStatusCompensating
	s.UpdatedAt = time.Now()
}

// MarkCompensated 标记补偿完成
func (s *SagaState) MarkCompensated() {
	s.Status = SagaStatusCompensated
	s.UpdatedAt = time.Now()
}

// IsCompleted 检查是否已完成
func (s *SagaState) IsCompleted() bool {
	return s.Status == SagaStatusCompleted
}

// IsFailed 检查是否已失败
func (s *SagaState) IsFailed() bool {
	return s.Status == SagaStatusFailed
}

// IsCompensating 检查是否正在补偿
func (s *SagaState) IsCompensating() bool {
	return s.Status == SagaStatusCompensating
}

// IsCompensated 检查是否已补偿
func (s *SagaState) IsCompensated() bool {
	return s.Status == SagaStatusCompensated
}

// IsRunning 检查是否正在运行
func (s *SagaState) IsRunning() bool {
	return s.Status == SagaStatusRunning
}

// SetData 设置自定义数据
//
// 参数：
//   - key: 键
//   - value: 值
func (s *SagaState) SetData(key string, value any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}
	s.Data[key] = value
	s.UpdatedAt = time.Now()
}

// GetData 获取自定义数据
//
// 参数：
//   - key: 键
//
// 返回：
//   - any: 值
//   - bool: 是否存在
func (s *SagaState) GetData(key string) (any, bool) {
	if s.Data == nil {
		return nil, false
	}
	val, ok := s.Data[key]
	return val, ok
}

// Clone 克隆状态
//
// 返回：
//   - *SagaState: 克隆的状态
func (s *SagaState) Clone() *SagaState {
	clone := &SagaState{
		SagaID:      s.SagaID,
		SagaType:    s.SagaType,
		CurrentStep: s.CurrentStep,
		Status:      s.Status,
		FailedStep:  s.FailedStep,
		Error:       s.Error,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}

	// 克隆 CompletedSteps
	clone.CompletedSteps = make([]string, len(s.CompletedSteps))
	copy(clone.CompletedSteps, s.CompletedSteps)

	// 克隆 Data
	if s.Data != nil {
		clone.Data = make(map[string]any)
		for k, v := range s.Data {
			clone.Data[k] = v
		}
	}

	return clone
}

// ToJSON 转换为 JSON
//
// 返回：
//   - []byte: JSON 数据
//   - error: 序列化错误
func (s *SagaState) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

// FromJSON 从 JSON 加载
//
// 参数：
//   - data: JSON 数据
//
// 返回：
//   - error: 反序列化错误
func (s *SagaState) FromJSON(data []byte) error {
	return json.Unmarshal(data, s)
}
