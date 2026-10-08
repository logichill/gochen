// Package main 演示能力装配、原子保存与提交后恢复，不依赖 Runtime。
package main

import "fmt"

func main() {
	atomicTransfer()
	recoverPublication()
	recoverOperationResult()
	rebuildSnapshot()
	publishOutbox()
	fmt.Println("all assembly and recovery scenarios passed")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
