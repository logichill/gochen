package contextx

import (
	"context"
	"strings"

	"gochen/errors"
)

type crossIsolationKey struct{}

// CrossIsolation 表达一次**显式的跨隔离空间只读开闸意图**。
//
// 存在意义：L2 隔离默认 fail-closed 且无条件生效，但对账、
// 计费汇总、跨隔离空间报表、平台巡检这类需求客观存在。没有正门时，这些需求
// 只会绕开仓储去手写原生 SQL——那样不仅丢掉隔离，还会一并丢掉软删除过滤、
// 字段映射与留痕。因此提供一个刺眼、必须显式声明、且必然留痕的入口。
//
// 本凭据**单独存在完全无效**：仓储还必须在构造期显式声明允许被开闸
// （SQL 侧 ormrepo.WithCrossIsolationReads，内存侧 testkit.WithMemoryCrossIsolationReads）。
// 两道闸都合上才放行，任何一侧缺失都按常规隔离处理。
//
// 边界：只放行读路径。写路径不受本凭据影响，且在已开闸的仓储上直接拒绝
// （见各仓储的写守卫），跨隔离写必须改为按目标隔离键逐个派生 context。
type CrossIsolation struct {
	// Reason 开闸的业务原因，必填。它是留痕里唯一能解释"为什么允许"的字段，
	// 因此不接受空值——留不下理由的开闸等同于静默开闸。
	Reason string

	// Operator 发起开闸的主体（人或作业），必填。
	//
	// 不复用 contextx.Operator：平台作业往往根本没有请求级 operator，
	// 而开闸留痕恰恰必须回答"谁开的"，缺省回退会把这个字段变成空字符串。
	Operator string
}

// Normalize 返回去除首尾空白后的凭据。
func (c CrossIsolation) Normalize() CrossIsolation {
	c.Reason = strings.TrimSpace(c.Reason)
	c.Operator = strings.TrimSpace(c.Operator)
	return c
}

// WithCrossIsolation 返回携带跨隔离只读开闸凭据的 context。
//
// Reason 与 Operator 必须非空，否则返回 InvalidInput——凭据的全部价值就是
// 留下"谁为了什么开的闸"，任何一项缺失都让留痕失去意义。
func WithCrossIsolation(ctx context.Context, credential CrossIsolation) (context.Context, error) {
	ctx, err := Ensure(ctx)
	if err != nil {
		return nil, err
	}
	credential = credential.Normalize()
	if credential.Reason == "" {
		return nil, errors.NewCode(errors.InvalidInput, "cross-isolation credential requires a reason")
	}
	if credential.Operator == "" {
		return nil, errors.NewCode(errors.InvalidInput, "cross-isolation credential requires an operator")
	}
	return context.WithValue(ctx, crossIsolationKey{}, credential), nil
}

// CrossIsolationFrom 读取当前 context 上的跨隔离只读开闸凭据。
//
// 未开闸时返回零值与 false。调用方**不得**把零值解释为"允许"。
func CrossIsolationFrom(ctx context.Context) (CrossIsolation, bool) {
	if ctx == nil {
		return CrossIsolation{}, false
	}
	credential, ok := ctx.Value(crossIsolationKey{}).(CrossIsolation)
	if !ok {
		return CrossIsolation{}, false
	}
	credential = credential.Normalize()
	if credential.Reason == "" || credential.Operator == "" {
		return CrossIsolation{}, false
	}
	return credential, true
}
