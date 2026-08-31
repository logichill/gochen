package testkit

import (
	"context"

	"gochen/contextx"
	"gochen/domain"
	"gochen/errors"
)

// L2 跨隔离只读阀门的内存实现。
//
// 与 runtime/db/orm/repo/cross_isolation.go 保持**相同语义**：
//   - 两道闸（构造期声明 + 请求期凭据）都合上才放行，缺一按常规隔离处理；
//   - 只放行读；写路径在已声明阀门的仓储上直接 Forbidden；
//   - 放行必然留痕，留痕失败即放弃本次读取。
//
// 与 SQL 侧的形态差异：内存仓储没有"列"与物理表，因此留痕里只有开闸凭据本身，
// 没有 Table / IsolationColumn 字段。
//
// 两侧按同一组语义断言编写特征测试（见 memory_repository_cross_isolation_test.go
// 与 runtime 的 cross_isolation_test.go）。

// CrossIsolationRead 描述一次**已放行**的跨隔离空间读取。
type CrossIsolationRead struct {
	// Reason 开闸原因，来自 contextx.CrossIsolation。
	Reason string
	// Operator 开闸发起人，来自 contextx.CrossIsolation。
	Operator string
}

// ICrossIsolationAudit 接收跨隔离只读放行的留痕。
//
// 错误契约：返回错误即中止本次读取。留痕是开闸的对价，不是可选的旁路观测。
type ICrossIsolationAudit interface {
	RecordCrossIsolationRead(ctx context.Context, read CrossIsolationRead) error
}

// CrossIsolationAuditFunc 允许用函数直接实现 ICrossIsolationAudit。
type CrossIsolationAuditFunc func(ctx context.Context, read CrossIsolationRead) error

// RecordCrossIsolationRead 记录一次跨隔离只读放行。
func (f CrossIsolationAuditFunc) RecordCrossIsolationRead(ctx context.Context, read CrossIsolationRead) error {
	if f == nil {
		return errors.NewCode(
			errors.InvalidInput,
			"cross-isolation audit function cannot be nil; this indicates a wiring bug",
		)
	}
	return f(ctx, read)
}

// WithMemoryCrossIsolationReads 允许本仓储在携带显式开闸凭据的请求上跳过隔离过滤。
//
// 对应 SQL 侧的 ormrepo.WithCrossIsolationReads。audit 为 nil，或仓储未同时声明
// WithMemoryIsolation 时 NewMemoryRepository panic（测试替身的 fail-fast 等价于
// 生产侧构造期报错）。
func WithMemoryCrossIsolationReads[T domain.IEntity[ID], ID comparable](audit ICrossIsolationAudit) MemoryOption[T, ID] {
	return func(r *MemoryRepository[T, ID]) {
		if r == nil {
			return
		}
		// audit 为 nil 时也打开标记，交给构造期校验报错——静默丢弃声明
		// 会让调用方以为阀门已就位。
		r.crossIsolationReads = true
		r.crossIsolationAudit = audit
	}
}

// validateCrossIsolationDeclaration 拦下让开闸声明落空或无意义的配置。
func (r *MemoryRepository[T, ID]) validateCrossIsolationDeclaration() {
	if !r.crossIsolationReads {
		return
	}
	if r.crossIsolationAudit == nil {
		panic("testkit: memory cross-isolation reads require a non-nil audit sink")
	}
	if !r.isolated {
		panic("testkit: memory cross-isolation reads require WithMemoryIsolation")
	}
}

// crossIsolationRelaxed 判断本次读取是否满足两道闸，并在放行时完成留痕。
//
// 留痕先于放行：记不下就不放行，返回错误而不是降级为普通隔离读——
// 后者会让"审计沉默"表现为"一切正常"。
func (r *MemoryRepository[T, ID]) crossIsolationRelaxed(ctx context.Context) (bool, error) {
	if r == nil || !r.crossIsolationReads || !r.isolated {
		return false, nil
	}
	credential, ok := contextx.CrossIsolationFrom(ctx)
	if !ok {
		return false, nil
	}
	if r.crossIsolationAudit == nil {
		return false, errors.NewCode(errors.Forbidden, "cross-isolation read requires an audit sink")
	}
	if err := r.crossIsolationAudit.RecordCrossIsolationRead(ctx, CrossIsolationRead{
		Reason:   credential.Reason,
		Operator: credential.Operator,
	}); err != nil {
		return false, errors.Wrap(err, errors.Forbidden, "failed to record cross-isolation read")
	}
	return true, nil
}

// guardCrossIsolationWrite 在开闸期间拒绝本仓储上的一切写入。
//
// 只对**已声明允许开闸**的仓储生效：未声明的仓储上凭据本就是惰性的，
// 同一 ctx 里向别的仓储正常写入是合理用法，不该被牵连。
func (r *MemoryRepository[T, ID]) guardCrossIsolationWrite(ctx context.Context) error {
	if r == nil || !r.crossIsolationReads || !r.isolated {
		return nil
	}
	if _, ok := contextx.CrossIsolationFrom(ctx); !ok {
		return nil
	}
	return errors.NewCode(errors.Forbidden,
		"cross-isolation credential permits reads only; derive a per-isolation-space context before writing")
}
