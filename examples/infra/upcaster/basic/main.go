package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

// UserCreatedV3 表示升级链最终目标版本的事件载荷。
type UserCreatedV3 struct {
	FullName string `json:"full_name"`
}

type userCreatedV1ToV2 struct{}

// FromVersion 声明该升级器接收 v1 载荷。
func (userCreatedV1ToV2) FromVersion() int { return 1 }

// ToVersion 声明该升级器输出 v2 载荷。
func (userCreatedV1ToV2) ToVersion() int { return 2 }

// Upgrade 把 v1 的 `name` 字段迁移为 v2 的 `full_name`。
func (userCreatedV1ToV2) Upgrade(data map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	// v1: name -> v2: full_name
	if v, ok := out["name"]; ok {
		out["full_name"] = v
		delete(out, "name")
	}
	return out, nil
}

type userCreatedV2ToV3 struct{}

// FromVersion 声明该升级器接收 v2 载荷。
func (userCreatedV2ToV3) FromVersion() int { return 2 }

// ToVersion 声明该升级器输出 v3 载荷。
func (userCreatedV2ToV3) ToVersion() int { return 3 }

// Upgrade 演示无结构变化时也可以显式保留升级链节点。
func (userCreatedV2ToV3) Upgrade(data map[string]any) (map[string]any, error) {
	// 这里假设 v2->v3 无结构变化，仅演示链路
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out, nil
}

// main 演示旧版本事件在消费边界统一升级与 hydration 的过程。
func main() {
	log.SetPrefix("[upcaster_demo] ")
	ctx := context.Background()

	const eventType = "UserCreated"

	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()

	// 1) 注册事件类型与最新 schemaVersion（v3）
	must(reg.RegisterWithVersion(eventType, 3, func() any { return &UserCreatedV3{} }))

	// 2) 注册 Upcaster 升级链（v1->v2->v3）
	must(upgraders.Register(eventType, userCreatedV1ToV2{}))
	must(upgraders.Register(eventType, userCreatedV2ToV3{}))

	// 3) 模拟读到一条老事件（schemaVersion=1，payload 为 JSON bytes）
	rawV1, _ := json.Marshal(map[string]any{"name": "Alice"})
	evt := eventing.NewEvent[int64](1, "User", eventType, 1, json.RawMessage(rawV1), 1)

	// 4) 在消费边界统一升级 + hydration（JSON/map -> 强类型 payload）
	_, err := upcast.UpgradeEventPayload(ctx, reg, upgraders, evt)
	must(err)

	fmt.Printf("event_type=%s schema=%d payload=%s\n", evt.GetType(), evt.EventSchemaVersion(), evt.GetPayload().TypeName())

	p, ok := messaging.PayloadAs[*UserCreatedV3](evt.GetPayload())
	if !ok {
		log.Fatalf("unexpected payload type: %s", evt.GetPayload().TypeName())
	}
	fmt.Printf("full_name=%s\n", p.FullName)
}

// must 在示例里用于快速失败，避免重复展开错误处理。
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
