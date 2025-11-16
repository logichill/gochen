package eventsourced

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	cmd "gochen/messaging/command"
)

// 领域命令（用于测试）
type setValueCommand struct {
	id int64
	v  int
}

func (c *setValueCommand) AggregateID() int64 { return c.id }

func TestService_AsCommandMessageHandler(t *testing.T) {
	ctx := context.Background()
	// 准备基础仓储与服务
	store := NewMockEventStore()
	base, err := NewEventSourcedRepository(EventSourcedRepositoryOptions[*TestAggregate]{
		AggregateType: "TestAggregate",
		Factory:       func(id int64) *TestAggregate { return NewTestAggregate(id) },
		EventStore:    store,
	})
	require.NoError(t, err)
	svc, err := NewEventSourcedService(base, nil)
	require.NoError(t, err)

	// 注册命令处理：将值设置为 payload
	require.NoError(t, svc.RegisterCommandHandler(&setValueCommand{}, func(ctx context.Context, cmd IEventSourcedCommand, agg *TestAggregate) error {
		payload, _ := cmd.(*setValueCommand)
		evt := eventing.NewDomainEvent(agg.GetID(), agg.GetAggregateType(), "ValueSet", uint64(agg.GetVersion()+1), payload.v)
		return agg.ApplyAndRecord(evt)
	}))

	// 适配为 IMessageHandler，并模拟分发
	handler := svc.AsCommandMessageHandler("SetValue", func(c *cmd.Command) (IEventSourcedCommand, error) {
		v, _ := c.GetPayload().(int)
		return &setValueCommand{id: c.GetAggregateID(), v: v}, nil
	})

	// 构造命令消息（类型为 command，metadata 携带 command_type）
	message := cmd.NewCommand("cmd-1", "SetValue", 1001, "TestAggregate", 42)
	// 注意：NewCommand 已设置 Type=command 且写入 command_type 元数据

	err = handler.Handle(ctx, message)
	require.NoError(t, err)

	// 验证保存成功并可重放
	loaded, err := base.GetByID(ctx, 1001)
	require.NoError(t, err)
	require.Equal(t, 42, loaded.Value)
}
