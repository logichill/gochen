package saga

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/messaging/command"
)

// TestNewSagaStep 验证 NewSagaStep。
func TestNewSagaStep(t *testing.T) {
	commandFunc := func(ctx context.Context) (*command.Command, error) {
		return nil, nil
	}

	step := NewSagaStep("TestStep", commandFunc)

	assert.Equal(t, "TestStep", step.Name)
	assert.NotNil(t, step.Command)
	assert.Nil(t, step.Compensation)
	assert.Nil(t, step.OnSuccess)
	assert.Nil(t, step.OnFailure)
}

// TestSagaStep_ChainedMethods 验证 SagaStep ChainedMethods。
func TestSagaStep_ChainedMethods(t *testing.T) {
	commandFunc := func(ctx context.Context) (*command.Command, error) {
		return nil, nil
	}

	compensationFunc := func(ctx context.Context) (*command.Command, error) {
		return nil, nil
	}

	successCallback := func(ctx context.Context, stepName string, err error) error {
		return nil
	}

	failureCallback := func(ctx context.Context, stepName string, err error) error {
		return nil
	}

	step := NewSagaStep("TestStep", commandFunc).
		WithCompensation(compensationFunc).
		WithOnSuccess(successCallback).
		WithOnFailure(failureCallback)

	assert.Equal(t, "TestStep", step.Name)
	assert.NotNil(t, step.Command)
	assert.NotNil(t, step.Compensation)
	assert.NotNil(t, step.OnSuccess)
	assert.NotNil(t, step.OnFailure)
	assert.True(t, step.HasCompensation())
}

// TestSagaStep_HasCompensation 验证 SagaStep HasCompensation。
func TestSagaStep_HasCompensation(t *testing.T) {
	commandFunc := func(ctx context.Context) (*command.Command, error) {
		return nil, nil
	}

	// 无补偿
	step1 := NewSagaStep("Step1", commandFunc)
	assert.False(t, step1.HasCompensation())

	// 有补偿
	step2 := NewSagaStep("Step2", commandFunc).
		WithCompensation(commandFunc)
	assert.True(t, step2.HasCompensation())
}

// TestNewSagaState 验证 NewSagaState。
func TestNewSagaState(t *testing.T) {
	state := NewSagaState("saga-123", "OrderSaga")

	assert.Equal(t, "saga-123", state.SagaID)
	assert.Equal(t, "OrderSaga", state.SagaType)
	assert.Equal(t, 0, state.CurrentStep)
	assert.Equal(t, SagaStatusPending, state.Status)
	assert.Empty(t, state.CompletedSteps)
	assert.NotNil(t, state.Data)
	assert.False(t, state.CreatedAt.IsZero())
	assert.False(t, state.UpdatedAt.IsZero())
}

// TestSagaState_MarkStepCompleted 验证 SagaState MarkStepCompleted。
func TestSagaState_MarkStepCompleted(t *testing.T) {
	state := NewSagaState("saga-123", "OrderSaga")

	state.MarkStepCompleted("Step1")
	assert.Equal(t, 1, state.CurrentStep)
	assert.Contains(t, state.CompletedSteps, "Step1")

	state.MarkStepCompleted("Step2")
	assert.Equal(t, 2, state.CurrentStep)
	assert.Contains(t, state.CompletedSteps, "Step2")
	assert.Len(t, state.CompletedSteps, 2)
}

// TestSagaState_MarkStepFailed 验证 SagaState MarkStepFailed。
func TestSagaState_MarkStepFailed(t *testing.T) {
	state := NewSagaState("saga-123", "OrderSaga")

	err := assert.AnError
	state.MarkStepFailed("Step1", err)

	assert.Equal(t, "Step1", state.FailedStep)
	assert.Contains(t, state.Error, "assert.AnError")
	assert.Equal(t, SagaStatusFailed, state.Status)
}

// TestSagaState_StatusMethods 验证 SagaState StatusMethods。
func TestSagaState_StatusMethods(t *testing.T) {
	state := NewSagaState("saga-123", "OrderSaga")

	// Pending
	assert.False(t, state.IsCompleted())
	assert.False(t, state.IsFailed())
	assert.False(t, state.IsRunning())

	// Running
	state.Status = SagaStatusRunning
	assert.True(t, state.IsRunning())

	// Completed
	state.MarkCompleted()
	assert.True(t, state.IsCompleted())

	// Failed
	state2 := NewSagaState("saga-456", "OrderSaga")
	state2.MarkStepFailed("Step1", assert.AnError)
	assert.True(t, state2.IsFailed())

	// Compensating
	state3 := NewSagaState("saga-789", "OrderSaga")
	state3.MarkCompensating()
	assert.True(t, state3.IsCompensating())

	// Compensated
	state3.MarkCompensated()
	assert.True(t, state3.IsCompensated())
}

// TestSagaState_Data 验证 SagaState Data。
func TestSagaState_Data(t *testing.T) {
	state := NewSagaState("saga-123", "OrderSaga")

	// 设置数据
	state.SetData("orderID", "order-123")
	state.SetData("userID", "user-456")

	// 获取数据
	orderID, ok := state.GetData("orderID")
	assert.True(t, ok)
	assert.Equal(t, "order-123", orderID)

	userID, ok := state.GetData("userID")
	assert.True(t, ok)
	assert.Equal(t, "user-456", userID)

	// 不存在的数据
	_, ok = state.GetData("non-existent")
	assert.False(t, ok)
}

// TestSagaState_Clone 验证 SagaState Clone。
func TestSagaState_Clone(t *testing.T) {
	original := NewSagaState("saga-123", "OrderSaga")
	original.MarkStepCompleted("Step1")
	original.MarkStepCompleted("Step2")
	original.SetData("key", "value")
	original.SetData("nested", map[string]any{"a": []any{map[string]any{"b": "c"}}})
	original.SetData("bytes", []byte{1, 2, 3})
	original.SetData("strings", []string{"a", "b"})
	original.SetData("maps", []map[string]any{{"x": map[string]any{"y": "z"}}})

	cloned := original.Clone()

	assert.Equal(t, original.SagaID, cloned.SagaID)
	assert.Equal(t, original.CurrentStep, cloned.CurrentStep)
	assert.Equal(t, original.Status, cloned.Status)
	assert.Equal(t, len(original.CompletedSteps), len(cloned.CompletedSteps))

	// 修改克隆不影响原始
	cloned.MarkStepCompleted("Step3")
	assert.Equal(t, 2, original.CurrentStep)
	assert.Equal(t, 3, cloned.CurrentStep)

	// 修改克隆的嵌套 Data 不影响原始
	nestedClone, ok := cloned.Data["nested"].(map[string]any)
	require.True(t, ok)
	listClone, ok := nestedClone["a"].([]any)
	require.True(t, ok)
	itemClone, ok := listClone[0].(map[string]any)
	require.True(t, ok)
	itemClone["b"] = "changed"

	nestedOrig, ok := original.Data["nested"].(map[string]any)
	require.True(t, ok)
	listOrig, ok := nestedOrig["a"].([]any)
	require.True(t, ok)
	itemOrig, ok := listOrig[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "c", itemOrig["b"])

	// 覆盖 []byte / []string / []map[string]any 的深拷贝
	b1 := cloned.Data["bytes"].([]byte)
	b1[0] = 9
	require.Equal(t, byte(1), original.Data["bytes"].([]byte)[0])

	ss := cloned.Data["strings"].([]string)
	ss[0] = "changed"
	require.Equal(t, "a", original.Data["strings"].([]string)[0])

	mm := cloned.Data["maps"].([]map[string]any)
	mm[0]["x"].(map[string]any)["y"] = "changed"
	require.Equal(t, "z", original.Data["maps"].([]map[string]any)[0]["x"].(map[string]any)["y"])
}

// TestSagaState_JSON 验证 SagaState JSON。
func TestSagaState_JSON(t *testing.T) {
	state := NewSagaState("saga-123", "OrderSaga")
	state.MarkStepCompleted("Step1")
	state.SetData("key", "value")

	// 序列化
	data, err := state.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// 反序列化
	state2 := &SagaState{}
	err = state2.FromJSON(data)
	require.NoError(t, err)

	assert.Equal(t, state.SagaID, state2.SagaID)
	assert.Equal(t, state.CurrentStep, state2.CurrentStep)
	assert.Equal(t, len(state.CompletedSteps), len(state2.CompletedSteps))
}

// TestMemorySagaStateStore 验证 MemorySagaStateStore。
func TestMemorySagaStateStore(t *testing.T) {
	store := NewMemorySagaStateStore()
	ctx := context.Background()

	state := NewSagaState("saga-123", "OrderSaga")

	// Save
	err := store.Save(ctx, state)
	require.NoError(t, err)

	// Load
	loaded, err := store.Load(ctx, "saga-123")
	require.NoError(t, err)
	assert.Equal(t, state.SagaID, loaded.SagaID)

	// Update
	state.MarkStepCompleted("Step1")
	err = store.Update(ctx, state)
	require.NoError(t, err)

	loaded2, err := store.Load(ctx, "saga-123")
	require.NoError(t, err)
	assert.Equal(t, 1, loaded2.CurrentStep)

	// Delete
	err = store.Delete(ctx, "saga-123")
	require.NoError(t, err)

	_, err = store.Load(ctx, "saga-123")
	require.True(t, errors.Is(err, errors.NotFound))
}

func TestMemorySagaStateStore_ClearAndCount(t *testing.T) {
	store := NewMemorySagaStateStore()
	ctx := context.Background()

	require.Equal(t, 0, store.Count())
	require.NoError(t, store.Save(ctx, NewSagaState("s1", "t")))
	require.NoError(t, store.Save(ctx, NewSagaState("s2", "t")))
	require.Equal(t, 2, store.Count())

	store.Clear()
	require.Equal(t, 0, store.Count())
}

// TestMemorySagaStateStore_UpdateRequiresExisting 验证 MemorySagaStateStore UpdateRequiresExisting。
func TestMemorySagaStateStore_UpdateRequiresExisting(t *testing.T) {
	store := NewMemorySagaStateStore()
	ctx := context.Background()

	state := NewSagaState("missing", "OrderSaga")
	err := store.Update(ctx, state)
	require.True(t, errors.Is(err, errors.NotFound))
}

func TestMemorySagaStateStore_SaveRequiresMissing(t *testing.T) {
	store := NewMemorySagaStateStore()
	ctx := context.Background()

	original := NewSagaState("dup", "OrderSaga")
	require.NoError(t, store.Save(ctx, original))

	replacement := NewSagaState("dup", "OtherSaga")
	replacement.MarkStepCompleted("step1")

	err := store.Save(ctx, replacement)
	require.True(t, errors.Is(err, errors.Conflict))

	loaded, loadErr := store.Load(ctx, "dup")
	require.NoError(t, loadErr)
	assert.Equal(t, "OrderSaga", loaded.SagaType)
	assert.Zero(t, loaded.CurrentStep)
}

func TestMemorySagaStateStore_List(t *testing.T) {
	store := NewMemorySagaStateStore()
	ctx := context.Background()

	// 创建多个状态
	state1 := NewSagaState("saga-1", "OrderSaga")
	state1.Status = SagaStatusCompleted
	store.Save(ctx, state1)

	state2 := NewSagaState("saga-2", "OrderSaga")
	state2.Status = SagaStatusFailed
	store.Save(ctx, state2)

	state3 := NewSagaState("saga-3", "OrderSaga")
	state3.Status = SagaStatusRunning
	store.Save(ctx, state3)

	// 列出所有
	all, err := store.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// 按状态过滤
	completed, err := store.List(ctx, SagaStatusCompleted)
	require.NoError(t, err)
	assert.Len(t, completed, 1)
	assert.Equal(t, "saga-1", completed[0].SagaID)

	failed, err := store.List(ctx, SagaStatusFailed)
	require.NoError(t, err)
	assert.Len(t, failed, 1)
	assert.Equal(t, "saga-2", failed[0].SagaID)
}

// BenchmarkSagaState_Create 用于评估 SagaState Create 的性能。
func BenchmarkSagaState_Create(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewSagaState("saga-123", "OrderSaga")
	}
}

// BenchmarkSagaState_Clone 用于评估 SagaState Clone 的性能。
func BenchmarkSagaState_Clone(b *testing.B) {
	state := NewSagaState("saga-123", "OrderSaga")
	state.MarkStepCompleted("Step1")
	state.MarkStepCompleted("Step2")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = state.Clone()
	}
}
