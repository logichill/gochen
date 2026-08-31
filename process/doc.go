// Package process 用于承载“一个过程如何被提交、推进、收敛、恢复与观察”的框架能力。
// 它面向的是“多阶段性质”的运行时问题，而不是某个具体业务域。
//
// 子包：
//   - process/saga: 补偿型编排
//   - process/workflow: 状态机与流程推进
//   - process/lock: 串行化执行与分布式锁抽象
//   - process/task: 任务抽象与调度
package process
