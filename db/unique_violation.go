package db

import (
	"errors"
	"reflect"
	"strings"
)

const maxUniqueViolationWalkDepth = 32

// IsUniqueViolation 判断错误是否为唯一键/主键冲突。
//
// 检测分层（按可靠性从高到低）：
//  1. 驱动暴露的结构化错误码（SQLite ExtendedCode/Code、postgres/mysql 错误结构）；
//  2. SQLSTATE 文本（postgres 23505）；
//  3. 跨方言唯一冲突错误文本兜底。
//
// 仅识别精确的唯一/主键冲突信号，不接受 SQLite 基础码 19 或 MySQL 通用完整性
// 约束 SQLSTATE 23000/23001，避免把 NOT NULL/CHECK/FK 等错误误判为唯一冲突。
//
// 仅能拿到错误字符串的调用方应优先复用本函数，避免各自维护脆弱的文本匹配。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if code, ok := driverConstraintCode(err); ok && isUniqueConstraintCode(code) {
		return true
	}
	if state, ok := driverSQLState(err); ok && state == "23505" {
		return true
	}
	if number, ok := driverErrorNumber(err); ok && number == 1062 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate entry") || // mysql
		strings.Contains(msg, "duplicate key") || // postgres/通用
		strings.Contains(msg, "violates unique constraint") || // postgres
		strings.Contains(msg, "unique constraint") || // postgres/通用
		strings.Contains(msg, "unique constraint failed") || // sqlite
		strings.Contains(msg, "sqlstate 23505") // postgres unique_violation
}

// driverConstraintCode 通过 Code()/ExtendedCode 方法或字段提取驱动级约束错误码。
func driverConstraintCode(err error) (int64, bool) {
	if err == nil {
		return 0, false
	}
	if code, ok := constraintCodeByMethod(err); ok {
		return code, true
	}
	var found int64
	var ok bool
	walkUniqueViolationErrors(err, maxUniqueViolationWalkDepth, func(current error) bool {
		if code, exists := constraintCodeByValue(reflect.ValueOf(current)); exists {
			found = code
			ok = true
			return true
		}
		return false
	})
	return found, ok
}

func constraintCodeByMethod(err error) (int64, bool) {
	type codeInt interface{ Code() int }
	var withInt codeInt
	if errors.As(err, &withInt) && withInt != nil {
		return int64(withInt.Code()), true
	}
	type codeInt64 interface{ Code() int64 }
	var withInt64 codeInt64
	if errors.As(err, &withInt64) && withInt64 != nil {
		return withInt64.Code(), true
	}
	type codeUint interface{ Code() uint }
	var withUint codeUint
	if errors.As(err, &withUint) && withUint != nil {
		return int64(withUint.Code()), true
	}
	return 0, false
}

func constraintCodeByValue(value reflect.Value) (int64, bool) {
	for value.IsValid() {
		if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return 0, false
			}
			value = value.Elem()
			continue
		}
		break
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0, false
	}
	if code, ok := numericFieldValue(value, "ExtendedCode"); ok && code != 0 {
		return code, true
	}
	return numericFieldValue(value, "Code")
}

func driverSQLState(err error) (string, bool) {
	type sqlStateMethod interface{ SQLState() string }
	var withState sqlStateMethod
	if errors.As(err, &withState) && withState != nil {
		state := strings.TrimSpace(withState.SQLState())
		if state != "" {
			return state, true
		}
	}
	var found string
	var ok bool
	walkUniqueViolationErrors(err, maxUniqueViolationWalkDepth, func(current error) bool {
		if state, exists := stringFieldValue(reflect.ValueOf(current), "SQLState"); exists && state != "" {
			found = state
			ok = true
			return true
		}
		return false
	})
	return found, ok
}

func driverErrorNumber(err error) (int64, bool) {
	var found int64
	var ok bool
	walkUniqueViolationErrors(err, maxUniqueViolationWalkDepth, func(current error) bool {
		value := reflect.ValueOf(current)
		for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
			if value.IsNil() {
				return false
			}
			value = value.Elem()
		}
		if !value.IsValid() || value.Kind() != reflect.Struct {
			return false
		}
		for _, name := range []string{"Number", "Errno"} {
			if number, exists := numericFieldValue(value, name); exists {
				found = number
				ok = true
				return true
			}
		}
		return false
	})
	return found, ok
}

func numericFieldValue(value reflect.Value, name string) (int64, bool) {
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int64(field.Uint()), true
	default:
		return 0, false
	}
}

func stringFieldValue(value reflect.Value, name string) (string, bool) {
	for value.IsValid() {
		if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return "", false
			}
			value = value.Elem()
			continue
		}
		break
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", false
	}
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() || field.Kind() != reflect.String {
		return "", false
	}
	return strings.TrimSpace(field.String()), true
}

func walkUniqueViolationErrors(err error, maxDepth int, visit func(error) bool) bool {
	if err == nil || maxDepth <= 0 {
		return false
	}
	if visit(err) {
		return true
	}
	type unwrapOne interface{ Unwrap() error }
	if single, ok := err.(unwrapOne); ok {
		if walkUniqueViolationErrors(single.Unwrap(), maxDepth-1, visit) {
			return true
		}
	}
	type unwrapMany interface{ Unwrap() []error }
	if many, ok := err.(unwrapMany); ok {
		for _, nested := range many.Unwrap() {
			if walkUniqueViolationErrors(nested, maxDepth-1, visit) {
				return true
			}
		}
	}
	return false
}

// isUniqueConstraintCode 仅覆盖精确的唯一/主键冲突错误码：
// SQLite 1555=PRIMARYKEY、2067=UNIQUE（均为扩展码）。
//
// 不接受 SQLite 基础码 19(SQLITE_CONSTRAINT)：该大类还包含 NOT NULL/CHECK/FK 等，
// 不等价于唯一冲突；把它当作唯一冲突会让真实数据错误被 outbox/event store 的
// duplicate/idempotent 分支静默吞掉。本项目默认驱动 modernc.org/sqlite 开机即启用
// 扩展结果码，唯一冲突会直接以 2067/1555 暴露，无需依赖基础码兜底。
func isUniqueConstraintCode(code int64) bool {
	switch code {
	case 1555, 2067:
		return true
	default:
		return false
	}
}
