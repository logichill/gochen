package main

import (
	"context"
	"fmt"
	"log"

	"gochen/examples/patterns/saga/basic/internal/transfer"
)

func main() {
	log.SetPrefix("[saga_demo] ")
	ctx := context.Background()
	orch := transfer.NewOrchestrator()

	// 启动流程
	id, err := orch.StartTransfer(ctx, 1001, 2002, 50)
	must(err)
	must(orch.Debit(ctx, id))

	// 情景一：成功
	must(orch.Credit(ctx, id, true))
	st, _ := orch.GetState(ctx, id)
	fmt.Printf("TX %s -> step=%v status=%v\n", id, st.Data["step"], st.Data["status"])

	// 情景二：失败补偿
	id2, err := orch.StartTransfer(ctx, 1001, 2002, 75)
	must(err)
	must(orch.Debit(ctx, id2))
	_ = orch.Credit(ctx, id2, false)
	st2, _ := orch.GetState(ctx, id2)
	fmt.Printf("TX %s -> step=%v status=%v\n", id2, st2.Data["step"], st2.Data["status"])
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
