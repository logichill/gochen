package action

import (
	"gochen/errors"
)

// Operation 表示一次 CRUD 应用层操作。
type Operation string

const (
	OpCreate Operation = "create"
	OpRead   Operation = "read"
	OpUpdate Operation = "update"
	OpDelete Operation = "delete"
	OpList   Operation = "list"

	// 审计扩展的高危操作。
	// 它们不属于基础 CRUD，只有 audited Application 才会用到；
	// 未配置即拒绝，语义与基础操作一致。
	OpRestore   Operation = "restore"
	OpPurge     Operation = "purge"
	OpAuditRead Operation = "audit_read"
)

// OperationPolicy 把 CRUD 操作映射到三段式权限码。
//
// Fail-Closed 语义（§4.1）：字段为空表示该操作**没有配置**，一律拒绝，
// 绝不解释为"无需鉴权"。若某操作确实不需要动作级管控，
// 不要把它接入 L1 装饰器。
type OperationPolicy struct {
	Create string // 例 "order:api:create"
	Read   string
	Update string
	Delete string
	List   string

	// Restore / Purge / AuditRead 对应审计扩展的高危操作。
	// 只有装配 audited Application 时才需要填写。
	Restore   string
	Purge     string
	AuditRead string
}

// CodeFor 返回指定操作对应的权限码。
func (p OperationPolicy) CodeFor(op Operation) (string, bool) {
	var code string
	switch op {
	case OpCreate:
		code = p.Create
	case OpRead:
		code = p.Read
	case OpUpdate:
		code = p.Update
	case OpDelete:
		code = p.Delete
	case OpList:
		code = p.List
	case OpRestore:
		code = p.Restore
	case OpPurge:
		code = p.Purge
	case OpAuditRead:
		code = p.AuditRead
	default:
		return "", false
	}
	if code == "" {
		return "", false
	}
	return code, true
}

// Validate 校验策略中所有已配置的权限码均满足三段式约束。
//
// 装配期调用，把拼写错误暴露在启动期而不是请求期。
// 空策略同样拒绝：一个字段都没配置的装饰器在请求期只会恒 Forbidden，
// 那是装配失误，应在启动期暴露。
func (p OperationPolicy) Validate() error {
	if p.isEmpty() {
		return errors.NewCode(errors.InvalidInput, "operation policy is empty; every operation would be denied")
	}
	for _, entry := range []struct {
		op   Operation
		code string
	}{
		{OpCreate, p.Create},
		{OpRead, p.Read},
		{OpUpdate, p.Update},
		{OpDelete, p.Delete},
		{OpList, p.List},
		{OpRestore, p.Restore},
		{OpPurge, p.Purge},
		{OpAuditRead, p.AuditRead},
	} {
		if entry.code == "" {
			continue
		}
		if !IsValidCode(entry.code) {
			return errors.NewCode(errors.InvalidInput, "invalid action code in operation policy").
				WithContext("operation", string(entry.op)).
				WithContext("action", entry.code)
		}
	}
	return nil
}

func (p OperationPolicy) isEmpty() bool {
	return p.Create == "" && p.Read == "" && p.Update == "" && p.Delete == "" &&
		p.List == "" && p.Restore == "" && p.Purge == "" && p.AuditRead == ""
}
