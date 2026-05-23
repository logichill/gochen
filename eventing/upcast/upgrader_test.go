package upcast

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/messaging"
)

type v3Payload struct {
	FullName string `json:"full_name"`
}

type upgradeV1ToV2 struct{}

// FromVersion result：数量/计数。
//
// 返回：
func (u upgradeV1ToV2) FromVersion() int { return 1 }

// ToVersion result：数量/计数。
//
// 返回：
func (u upgradeV1ToV2) ToVersion() int { return 2 }

// Upgrade data：数据（载荷/对象）（类型：map[string]any）。
//
// 参数：
//
// 返回：
// - result1：映射结果（类型：map[string]any）
// - err：错误信息（nil 表示成功）
func (u upgradeV1ToV2) Upgrade(data map[string]any) (map[string]any, error) {
	out := cloneMap(data)
	if v, ok := out["name"]; ok {
		out["full_name"] = v
		delete(out, "name")
	}
	return out, nil
}

type upgradeV2ToV3 struct{}

// FromVersion result：数量/计数。
//
// 返回：
func (u upgradeV2ToV3) FromVersion() int { return 2 }

// ToVersion result：数量/计数。
//
// 返回：
func (u upgradeV2ToV3) ToVersion() int { return 3 }

// Upgrade data：数据（载荷/对象）（类型：map[string]any）。
//
// 参数：
//
// 返回：
// - result1：映射结果（类型：map[string]any）
// - err：错误信息（nil 表示成功）
func (u upgradeV2ToV3) Upgrade(data map[string]any) (map[string]any, error) {
	return cloneMap(data), nil
}

// TestUpgradeEventData_ChainApplied 验证 UpgradeEventData ChainApplied。
func TestUpgradeEventData_ChainApplied(t *testing.T) {
	const eventType = "UpgraderTestEvent"

	reg := registry.NewRegistry()
	upgraders := NewUpgraderRegistry()

	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeV1ToV2{}); err != nil {
		t.Fatalf("register upgrader v1->v2: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeV2ToV3{}); err != nil {
		t.Fatalf("register upgrader v2->v3: %v", err)
	}

	got, ver, err := UpgradeEventData(reg, upgraders, eventType, 1, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if ver != 3 {
		t.Fatalf("expected version 3, got %d", ver)
	}
	if got["full_name"] != "alice" {
		t.Fatalf("expected full_name=alice, got %#v", got)
	}
}

// TestUpgradeEventData_MissingUpgraderFails 验证 UpgradeEventData MissingUpgraderFails。
func TestUpgradeEventData_MissingUpgraderFails(t *testing.T) {
	const eventType = "UpgraderMissingLinkEvent"
	reg := registry.NewRegistry()
	upgraders := NewUpgraderRegistry()

	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeV1ToV2{}); err != nil {
		t.Fatalf("register upgrader v1->v2: %v", err)
	}

	_, _, err := UpgradeEventData(reg, upgraders, eventType, 1, map[string]any{"name": "alice"})
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestUpgradeEventPayload_UpgradesAndHydratesFromJSONBytes 验证 UpgradeEventPayload UpgradesAndHydratesFromJSONBytes。
func TestUpgradeEventPayload_UpgradesAndHydratesFromJSONBytes(t *testing.T) {
	const eventType = "UpgraderHydrateEvent"
	reg := registry.NewRegistry()
	upgraders := NewUpgraderRegistry()

	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeV1ToV2{}); err != nil {
		t.Fatalf("register upgrader v1->v2: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeV2ToV3{}); err != nil {
		t.Fatalf("register upgrader v2->v3: %v", err)
	}

	raw, _ := json.Marshal(map[string]any{"name": "alice"})
	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, json.RawMessage(raw), 1)

	if _, err := UpgradeEventPayload(context.Background(), reg, upgraders, evt); err != nil {
		t.Fatalf("upgrade payload: %v", err)
	}
	if evt.EventSchemaVersion() != 3 {
		t.Fatalf("expected schema version 3, got %d", evt.EventSchemaVersion())
	}
	typed, ok := messaging.PayloadAs[*v3Payload](evt.GetPayload())
	if !ok {
		t.Fatalf("expected typed payload, got %s", evt.GetPayload().TypeName())
	}
	if typed.FullName != "alice" {
		t.Fatalf("expected FullName=alice, got %q", typed.FullName)
	}
}

// TestHydrateEventPayload_UsesEventSchemaVersion 验证 HydrateEventPayload 使用事件自身 schema 版本。
func TestHydrateEventPayload_UsesEventSchemaVersion(t *testing.T) {
	const eventType = "HydrateSchemaVersionEvent"
	reg := registry.NewRegistry()
	upgraders := NewUpgraderRegistry()

	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeV2ToV3{}); err != nil {
		t.Fatalf("register upgrader v2->v3: %v", err)
	}

	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, map[string]any{"full_name": "alice"}, 2)
	payload, ok, err := HydrateEventPayload(reg, upgraders, evt)
	if err != nil {
		t.Fatalf("hydrate payload: %v", err)
	}
	if !ok {
		t.Fatalf("expected hydrated payload")
	}
	typed, ok := payload.(*v3Payload)
	if !ok {
		t.Fatalf("expected *v3Payload, got %T", payload)
	}
	if typed.FullName != "alice" {
		t.Fatalf("expected FullName=alice, got %q", typed.FullName)
	}
}

func TestHydrateEventPayload_CurrentSchemaMapAllowsNilUpgraders(t *testing.T) {
	const eventType = "HydrateCurrentSchemaMapEvent"
	reg := registry.NewRegistry()
	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}

	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, map[string]any{"full_name": "alice"}, 3)
	payload, ok, err := HydrateEventPayload(reg, nil, evt)
	if err != nil {
		t.Fatalf("hydrate payload: %v", err)
	}
	if !ok {
		t.Fatalf("expected hydrated payload")
	}
	typed, ok := payload.(*v3Payload)
	if !ok {
		t.Fatalf("expected *v3Payload, got %T", payload)
	}
	if typed.FullName != "alice" {
		t.Fatalf("expected FullName=alice, got %q", typed.FullName)
	}
}

func TestDecodeEventPayload_CurrentSchemaJSONAllowsNilUpgraders(t *testing.T) {
	const eventType = "DecodeCurrentSchemaJSONEvent"
	reg := registry.NewRegistry()
	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}

	raw, err := json.Marshal(map[string]any{"full_name": "alice"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, json.RawMessage(raw), 3)
	payload, ok, err := DecodeEventPayload[*v3Payload](reg, nil, evt)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !ok {
		t.Fatalf("expected decoded payload")
	}
	if payload.FullName != "alice" {
		t.Fatalf("expected FullName=alice, got %q", payload.FullName)
	}
}

func TestDecodeEventPayload_CurrentSchemaJSONDecodeErrorDoesNotRequireUpgraders(t *testing.T) {
	const eventType = "DecodeCurrentSchemaJSONDecodeErrorEvent"
	reg := registry.NewRegistry()
	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}

	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, json.RawMessage(`{"full_name":123}`), 3)
	_, _, err := DecodeEventPayload[*v3Payload](reg, nil, evt)
	if err == nil {
		t.Fatalf("expected decode error")
	}
	if strings.Contains(err.Error(), "upgrader registry") {
		t.Fatalf("expected decode error, got upgrader error: %v", err)
	}
}

func TestHydrateEventPayload_OldSchemaRequiresUpgraders(t *testing.T) {
	const eventType = "HydrateOldSchemaRequiresUpgradersEvent"
	reg := registry.NewRegistry()
	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3Payload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}

	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, map[string]any{"name": "alice"}, 2)
	_, _, err := HydrateEventPayload(reg, nil, evt)
	if err == nil {
		t.Fatalf("expected nil upgraders to fail for old schema")
	}
}

// TestDecodeEventPayload_NormalizesStructValue 验证 struct 值 payload 可解码为指针类型。
func TestDecodeEventPayload_NormalizesStructValue(t *testing.T) {
	const eventType = "DecodeStructValueEvent"
	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, v3Payload{FullName: "alice"}, 1)

	payload, ok, err := DecodeEventPayload[*v3Payload](nil, nil, evt)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !ok {
		t.Fatalf("expected decoded payload")
	}
	if payload.FullName != "alice" {
		t.Fatalf("expected FullName=alice, got %q", payload.FullName)
	}
}

type v3User struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
}

type v3Role struct {
	Name string `json:"name"`
}

type v3ComplexPayload struct {
	User  v3User   `json:"user"`
	Roles []v3Role `json:"roles"`
}

type v3SplitMergePayload struct {
	Name struct {
		First string `json:"first"`
		Last  string `json:"last"`
	} `json:"name"`
	Age int `json:"age"`
}

type upgradeSplitMergeV1ToV2 struct{}

// FromVersion result：数量/计数。
//
// 返回：
func (u upgradeSplitMergeV1ToV2) FromVersion() int { return 1 }

// ToVersion result：数量/计数。
//
// 返回：
func (u upgradeSplitMergeV1ToV2) ToVersion() int { return 2 }

// Upgrade data：数据（载荷/对象）（类型：map[string]any）。
//
// 参数：
//
// 返回：
// - result1：映射结果（类型：map[string]any）
// - err：错误信息（nil 表示成功）
func (u upgradeSplitMergeV1ToV2) Upgrade(data map[string]any) (map[string]any, error) {
	out := cloneMap(data)

	// full_name -> first_name/last_name
	if v, ok := out["full_name"].(string); ok && v != "" {
		parts := strings.Fields(v)
		if len(parts) > 0 {
			out["first_name"] = parts[0]
		}
		if len(parts) > 1 {
			out["last_name"] = strings.Join(parts[1:], " ")
		}
		delete(out, "full_name")
	}

	// age: string/json.Number/float64 -> int
	switch v := out["age"].(type) {
	case string:
		if v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				out["age"] = n
			}
		}
	case json.Number:
		if n, err := v.Int64(); err == nil {
			out["age"] = int(n)
		}
	case float64:
		out["age"] = int(v)
	}

	return out, nil
}

type upgradeSplitMergeV2ToV3 struct{}

// FromVersion result：数量/计数。
//
// 返回：
func (u upgradeSplitMergeV2ToV3) FromVersion() int { return 2 }

// ToVersion result：数量/计数。
//
// 返回：
func (u upgradeSplitMergeV2ToV3) ToVersion() int { return 3 }

// Upgrade data：数据（载荷/对象）（类型：map[string]any）。
//
// 参数：
//
// 返回：
// - result1：映射结果（类型：map[string]any）
// - err：错误信息（nil 表示成功）
func (u upgradeSplitMergeV2ToV3) Upgrade(data map[string]any) (map[string]any, error) {
	out := cloneMap(data)

	name := map[string]any{}
	if first, ok := out["first_name"]; ok {
		name["first"] = first
		delete(out, "first_name")
	}
	if last, ok := out["last_name"]; ok {
		name["last"] = last
		delete(out, "last_name")
	}
	out["name"] = name

	return out, nil
}

type upgradeComplexV1ToV2 struct{}

// FromVersion result：数量/计数。
//
// 返回：
func (u upgradeComplexV1ToV2) FromVersion() int { return 1 }

// ToVersion result：数量/计数。
//
// 返回：
func (u upgradeComplexV1ToV2) ToVersion() int { return 2 }

// Upgrade data：数据（载荷/对象）（类型：map[string]any）。
//
// 参数：
//
// 返回：
// - result1：映射结果（类型：map[string]any）
// - err：错误信息（nil 表示成功）
func (u upgradeComplexV1ToV2) Upgrade(data map[string]any) (map[string]any, error) {
	out := cloneMap(data)

	// user_id（可能是 json.Number/float64/string）统一转为 string，并迁移到 user.id
	var userID string
	switch v := out["user_id"].(type) {
	case json.Number:
		userID = v.String()
	case float64:
		userID = fmt.Sprintf("%.0f", v)
	case string:
		userID = v
	case nil:
		// ignore
	default:
		userID = fmt.Sprint(v)
	}
	delete(out, "user_id")

	user := map[string]any{}
	if userID != "" {
		user["id"] = userID
	}
	out["user"] = user

	// name -> full_name（仍保持在顶层，下一步再收敛进 user）
	if v, ok := out["name"]; ok {
		out["full_name"] = v
		delete(out, "name")
	}

	return out, nil
}

type upgradeComplexV2ToV3 struct{}

// FromVersion result：数量/计数。
//
// 返回：
func (u upgradeComplexV2ToV3) FromVersion() int { return 2 }

// ToVersion result：数量/计数。
//
// 返回：
func (u upgradeComplexV2ToV3) ToVersion() int { return 3 }

// Upgrade data：数据（载荷/对象）（类型：map[string]any）。
//
// 参数：
//
// 返回：
// - result1：映射结果（类型：map[string]any）
// - err：错误信息（nil 表示成功）
func (u upgradeComplexV2ToV3) Upgrade(data map[string]any) (map[string]any, error) {
	out := cloneMap(data)

	// full_name 下移到 user.full_name
	if fullName, ok := out["full_name"]; ok {
		if user, ok := out["user"].(map[string]any); ok {
			user2 := cloneMap(user)
			user2["full_name"] = fullName
			out["user"] = user2
		} else {
			out["user"] = map[string]any{"full_name": fullName}
		}
		delete(out, "full_name")
	}

	// roles: ["admin","user"] -> [{"name":"admin"},{"name":"user"}]
	if rawRoles, ok := out["roles"]; ok {
		switch roles := rawRoles.(type) {
		case []any:
			newRoles := make([]any, 0, len(roles))
			for _, r := range roles {
				if s, ok := r.(string); ok {
					newRoles = append(newRoles, map[string]any{"name": s})
				}
			}
			out["roles"] = newRoles
		case []string:
			newRoles := make([]any, 0, len(roles))
			for _, s := range roles {
				newRoles = append(newRoles, map[string]any{"name": s})
			}
			out["roles"] = newRoles
		}
	}

	return out, nil
}

// TestUpgradeEventPayload_ComplexEvolution_NestedAndIDConversion 验证 UpgradeEventPayload ComplexEvolution NestedAndIDConversion。
func TestUpgradeEventPayload_ComplexEvolution_NestedAndIDConversion(t *testing.T) {
	const eventType = "UpgraderComplexEvent"
	reg := registry.NewRegistry()
	upgraders := NewUpgraderRegistry()

	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3ComplexPayload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeComplexV1ToV2{}); err != nil {
		t.Fatalf("register upgrader v1->v2: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeComplexV2ToV3{}); err != nil {
		t.Fatalf("register upgrader v2->v3: %v", err)
	}

	raw, _ := json.Marshal(map[string]any{
		"user_id": json.Number("123"),
		"name":    "alice",
		"roles":   []string{"admin", "user"},
	})
	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, json.RawMessage(raw), 1)

	if _, err := UpgradeEventPayload(context.Background(), reg, upgraders, evt); err != nil {
		t.Fatalf("upgrade payload: %v", err)
	}
	if evt.EventSchemaVersion() != 3 {
		t.Fatalf("expected schema version 3, got %d", evt.EventSchemaVersion())
	}
	typed, ok := messaging.PayloadAs[*v3ComplexPayload](evt.GetPayload())
	if !ok {
		t.Fatalf("expected typed payload, got %s", evt.GetPayload().TypeName())
	}
	if typed.User.ID != "123" {
		t.Fatalf("expected user.id=123, got %q", typed.User.ID)
	}
	if typed.User.FullName != "alice" {
		t.Fatalf("expected user.full_name=alice, got %q", typed.User.FullName)
	}
	if len(typed.Roles) != 2 || typed.Roles[0].Name != "admin" || typed.Roles[1].Name != "user" {
		t.Fatalf("unexpected roles: %#v", typed.Roles)
	}
}

// TestUpgradeEventPayload_SplitMergeAndTypeChange 验证 UpgradeEventPayload SplitMergeAndTypeChange。
func TestUpgradeEventPayload_SplitMergeAndTypeChange(t *testing.T) {
	const eventType = "UpgraderSplitMergeEvent"
	reg := registry.NewRegistry()
	upgraders := NewUpgraderRegistry()

	if err := reg.RegisterWithVersion(eventType, 3, func() any { return &v3SplitMergePayload{} }); err != nil {
		t.Fatalf("register event type: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeSplitMergeV1ToV2{}); err != nil {
		t.Fatalf("register upgrader v1->v2: %v", err)
	}
	if err := upgraders.Register(eventType, upgradeSplitMergeV2ToV3{}); err != nil {
		t.Fatalf("register upgrader v2->v3: %v", err)
	}

	raw, _ := json.Marshal(map[string]any{
		"full_name": "Alice Smith",
		"age":       "18",
	})
	evt := eventing.NewEvent[int64](1, "Agg", eventType, 1, json.RawMessage(raw), 1)

	if _, err := UpgradeEventPayload(context.Background(), reg, upgraders, evt); err != nil {
		t.Fatalf("upgrade payload: %v", err)
	}
	if evt.EventSchemaVersion() != 3 {
		t.Fatalf("expected schema version 3, got %d", evt.EventSchemaVersion())
	}
	typed, ok := messaging.PayloadAs[*v3SplitMergePayload](evt.GetPayload())
	if !ok {
		t.Fatalf("expected typed payload, got %s", evt.GetPayload().TypeName())
	}
	if typed.Name.First != "Alice" || typed.Name.Last != "Smith" {
		t.Fatalf("unexpected name: %#v", typed.Name)
	}
	if typed.Age != 18 {
		t.Fatalf("expected age=18, got %d", typed.Age)
	}
}
