package saga

import (
	"encoding/json"
	"time"

	"gochen/clock"
	"gochen/internal/clonevalue"
)

var defaultSagaClock = clock.NewRealClock()

// SagaStatus 表示 Saga 实例所处的阶段。
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

	// SagaStatusPendingCompletion 所有步骤已成功，但 OnComplete 完成回调失败，等待 Resume 重试
	SagaStatusPendingCompletion SagaStatus = "pending_completion"

	// SagaStatusCompensating 补偿中
	SagaStatusCompensating SagaStatus = "compensating"

	// SagaStatusCompensated 已补偿
	SagaStatusCompensated SagaStatus = "compensated"
)

// SagaState 保存 Saga 的执行进度、失败信息与附加数据。
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

	// clock 不参与序列化/持久化；用于让测试可以稳定控制时间推进。
	clock clock.IClock `json:"-" db:"-"`
}

// NewSagaState 创建一条新的 Saga 状态记录。
func NewSagaState(sagaID, sagaType string) *SagaState {
	return newSagaStateWithClock(sagaID, sagaType, defaultSagaClock)
}

func newSagaStateWithClock(sagaID, sagaType string, clk clock.IClock) *SagaState {
	if isNilSagaDependency(clk) {
		clk = defaultSagaClock
	}
	now := clk.Now()
	return &SagaState{
		SagaID:         sagaID,
		SagaType:       sagaType,
		CurrentStep:    0,
		Status:         SagaStatusPending,
		CompletedSteps: []string{},
		Data:           make(map[string]any),
		CreatedAt:      now,
		UpdatedAt:      now,
		clock:          clk,
	}
}

// WithClock 注入时钟（测试或需要可控时间的场景）。
func (s *SagaState) WithClock(clk clock.IClock) *SagaState {
	if s == nil {
		return s
	}
	if !isNilSagaDependency(clk) {
		s.clock = clk
	}
	return s
}

// now 返回当前 SagaState 绑定时钟的当前时间。
func (s *SagaState) now() time.Time {
	if s == nil || s.clock == nil {
		return defaultSagaClock.Now()
	}
	return s.clock.Now()
}

// MarkStepCompleted 记录某个步骤已经成功完成，并推进当前步骤索引。
func (s *SagaState) MarkStepCompleted(stepName string) {
	s.CompletedSteps = append(s.CompletedSteps, stepName)
	s.CurrentStep++
	s.UpdatedAt = s.now()
}

// MarkStepFailed 记录某个步骤失败，并把 Saga 标记为 failed。
func (s *SagaState) MarkStepFailed(stepName string, err error) {
	s.FailedStep = stepName
	s.Error = err.Error()
	s.Status = SagaStatusFailed
	s.UpdatedAt = s.now()
}

// MarkRunning 把 Saga 标记为执行中。
func (s *SagaState) MarkRunning() {
	s.Status = SagaStatusRunning
	s.UpdatedAt = s.now()
}

// MarkCompleted 把 Saga 标记为已完成。
func (s *SagaState) MarkCompleted() {
	s.Status = SagaStatusCompleted
	s.UpdatedAt = s.now()
}

// MarkFailed 把 Saga 标记为失败并记录错误。
func (s *SagaState) MarkFailed(err error) {
	if err != nil {
		s.Error = err.Error()
	} else {
		s.Error = "failed without error detail"
	}
	s.Status = SagaStatusFailed
	s.UpdatedAt = s.now()
}

// MarkPendingCompletion 在所有步骤成功、但 OnComplete 完成回调失败时标记为待完成。
// 该状态不同于终态 Failed：可被 Resume 重新执行 completeSaga 以重试 OnComplete。
func (s *SagaState) MarkPendingCompletion(err error) {
	if err != nil {
		s.Error = err.Error()
	} else {
		s.Error = "completion pending without error detail"
	}
	s.Status = SagaStatusPendingCompletion
	s.UpdatedAt = s.now()
}

// MarkCompensating 把 Saga 标记为补偿中。
func (s *SagaState) MarkCompensating() {
	s.Status = SagaStatusCompensating
	s.UpdatedAt = s.now()
}

// MarkCompensated 把 Saga 标记为补偿完成。
func (s *SagaState) MarkCompensated() {
	s.Status = SagaStatusCompensated
	s.UpdatedAt = s.now()
}

// IsCompleted 判断 Saga 是否已成功完成。
func (s *SagaState) IsCompleted() bool {
	return s.Status == SagaStatusCompleted
}

// IsFailed 判断 Saga 是否已失败。
func (s *SagaState) IsFailed() bool {
	return s.Status == SagaStatusFailed
}

// IsPendingCompletion 判断 Saga 是否处于完成回调待重试阶段。
func (s *SagaState) IsPendingCompletion() bool {
	return s.Status == SagaStatusPendingCompletion
}

// IsCompensating 判断 Saga 是否处于补偿阶段。
func (s *SagaState) IsCompensating() bool {
	return s.Status == SagaStatusCompensating
}

// IsCompensated 判断 Saga 是否已完成补偿。
func (s *SagaState) IsCompensated() bool {
	return s.Status == SagaStatusCompensated
}

// IsRunning 判断 Saga 是否仍在执行中。
func (s *SagaState) IsRunning() bool {
	return s.Status == SagaStatusRunning
}

// SetData 写入一项 Saga 附加数据。
func (s *SagaState) SetData(key string, value any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}
	s.Data[key] = value
	s.UpdatedAt = s.now()
}

// GetData 读取一项 Saga 附加数据。
func (s *SagaState) GetData(key string) (any, bool) {
	if s.Data == nil {
		return nil, false
	}
	val, ok := s.Data[key]
	return val, ok
}

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
		clock:       s.clock,
	}

	// 克隆 CompletedSteps
	clone.CompletedSteps = make([]string, len(s.CompletedSteps))
	copy(clone.CompletedSteps, s.CompletedSteps)

	// 克隆 Data
	if s.Data != nil {
		clone.Data = make(map[string]any)
		for k, v := range s.Data {
			clone.Data[k] = clonevalue.Clone(v)
		}
	}

	return clone
}

// ToJSON 转换为 JSON。
func (s *SagaState) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

// FromJSON 从 JSON 加载。
func (s *SagaState) FromJSON(data []byte) error {
	return json.Unmarshal(data, s)
}
