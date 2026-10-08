package registry

import (
	"encoding/json"
	"testing"

	"gochen/errors"
)

type sampleEvent struct {
	Name string `json:"name"`
}

// TestRegistry_RegisterWithVersion_RejectsTypedNilFactory 回归 R1d2-F01：
// 工厂返回「底层指针为 nil 的具名指针」（typed-nil 接口陷阱）时曾绕过注册期 nil 校验
// 静默成功，HasEvent 为 true 且不报错，但每次反序列化都必然失败并延迟到运行期才暴露；
// 注册期必须与字面 nil 工厂一样直接拒绝（InvalidInput），且不留下半注册状态。
func TestRegistry_RegisterWithVersion_RejectsTypedNilFactory(t *testing.T) {
	r := NewRegistry()

	// 对照组：字面 nil 工厂在修复前也已被拒绝（守卫存在，只是对 typed-nil 失效）。
	if err := r.RegisterWithVersion("LiteralNilEvent", 1, func() any { return nil }); err == nil {
		t.Fatal("expected literal-nil factory to be rejected, got nil error")
	} else if code := errors.Code(err); code != errors.InvalidInput {
		t.Fatalf("unexpected error code for literal-nil factory: %v", code)
	}
	if r.HasEvent("LiteralNilEvent") {
		t.Fatal("literal-nil factory must not leave a registered event type")
	}

	// 实验组：typed-nil 工厂必须被拒绝。
	typedNilFactory := func() any {
		var event *sampleEvent
		return event
	}
	if err := r.RegisterWithVersion("TypedNilEvent", 1, typedNilFactory); err == nil {
		t.Fatal("expected typed-nil factory to be rejected, got nil error")
	} else if code := errors.Code(err); code != errors.InvalidInput {
		t.Fatalf("unexpected error code for typed-nil factory: %v", code)
	}
	if r.HasEvent("TypedNilEvent") {
		t.Fatal("typed-nil factory must not leave a registered event type")
	}

	// Register 默认版本入口同样覆盖。
	if err := r.Register("TypedNilViaRegister", typedNilFactory); err == nil {
		t.Fatal("expected typed-nil factory via Register to be rejected, got nil error")
	} else if code := errors.Code(err); code != errors.InvalidInput {
		t.Fatalf("unexpected error code for typed-nil factory via Register: %v", code)
	}

	// 拒绝必须是无副作用的：注册表仍为空。
	if types := r.ListRegisteredTypes(); len(types) != 0 {
		t.Fatalf("registry must stay empty after rejections, got %v", types)
	}

	// 拒绝后事件类型名未被占用，可重新以合法工厂注册并正常反序列化。
	if err := r.RegisterWithVersion("TypedNilEvent", 1, func() any { return &sampleEvent{} }); err != nil {
		t.Fatalf("re-register with valid factory: %v", err)
	}
	typed, err := r.DeserializeFromMap("TypedNilEvent", map[string]any{"name": "demo"})
	if err != nil {
		t.Fatalf("deserialize after valid re-registration: %v", err)
	}
	ev, ok := typed.(*sampleEvent)
	if !ok || ev.Name != "demo" {
		t.Fatalf("unexpected instance %#v", typed)
	}
}

// TestRegistry_RegisterAndDeserialize 验证 Registry RegisterAndDeserialize。
func TestRegistry_RegisterAndDeserialize(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterWithVersion("TestEvent", 2, func() any { return &sampleEvent{} }); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !r.HasEvent("TestEvent") {
		t.Fatalf("expected type registered")
	}
	if got := r.EventSchemaVersion("TestEvent"); got != 2 {
		t.Fatalf("unexpected schema version: %d", got)
	}

	payload := map[string]any{"name": "demo"}
	typed, err := r.DeserializeFromMap("TestEvent", payload)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	ev, ok := typed.(*sampleEvent)
	if !ok {
		t.Fatalf("unexpected instance type %#v", typed)
	}
	if ev.Name != "demo" {
		t.Fatalf("unexpected value %s", ev.Name)
	}

	types := r.ListRegisteredTypes()
	if len(types) != 1 {
		t.Fatalf("unexpected types length %d", len(types))
	}
}

// snowflakeEvent 用于测试 int64 精度保持
type snowflakeEvent struct {
	UserID   int64   `json:"user_id"`
	TaskID   int64   `json:"task_id"`
	Amount   int64   `json:"amount"`
	Name     string  `json:"name"`
	SmallNum int     `json:"small_num"`
	FloatVal float64 `json:"float_val"`
}

// TestRegistry_DeserializeFromMap_Int64Precision 验证 Registry DeserializeFromMap Int64Precision。
func TestRegistry_DeserializeFromMap_Int64Precision(t *testing.T) {
	r := NewRegistry()

	// 注册事件类型
	err := r.Register("SnowflakeEvent", func() any { return &snowflakeEvent{} })
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// 模拟使用 UseNumber() 反序列化后的 map（数字为 json.Number）
	// 这是从 Outbox.ToEvent() 获取事件后，Payload 字段的实际形态
	snowflakeUserID := int64(7339797639495639040) // 19 位数字
	snowflakeTaskID := int64(7339797639495639041)

	payload := map[string]any{
		"user_id":   json.Number("7339797639495639040"), // json.Number 保持精度
		"task_id":   json.Number("7339797639495639041"),
		"amount":    json.Number("1234567890123456789"),
		"name":      "test",
		"small_num": json.Number("42"),
		"float_val": json.Number("3.14159"),
	}

	typed, err := r.DeserializeFromMap("SnowflakeEvent", payload)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}

	ev, ok := typed.(*snowflakeEvent)
	if !ok {
		t.Fatalf("unexpected instance type %T", typed)
	}

	// 验证 int64 精度保持
	if ev.UserID != snowflakeUserID {
		t.Errorf("UserID precision lost: expected %d, got %d", snowflakeUserID, ev.UserID)
	}
	if ev.TaskID != snowflakeTaskID {
		t.Errorf("TaskID precision lost: expected %d, got %d", snowflakeTaskID, ev.TaskID)
	}
	if ev.Amount != 1234567890123456789 {
		t.Errorf("Amount precision lost: expected %d, got %d", int64(1234567890123456789), ev.Amount)
	}
	if ev.Name != "test" {
		t.Errorf("Name mismatch: expected 'test', got '%s'", ev.Name)
	}
	if ev.SmallNum != 42 {
		t.Errorf("SmallNum mismatch: expected 42, got %d", ev.SmallNum)
	}
	if ev.FloatVal != 3.14159 {
		t.Errorf("FloatVal mismatch: expected 3.14159, got %f", ev.FloatVal)
	}
}
