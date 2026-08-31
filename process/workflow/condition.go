package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gochen/errors"
)

// IConditionEvaluator 定义工作流条件表达式求值接口。
type IConditionEvaluator interface {
	// Evaluate 评估条件表达式在给定数据上下文中的真假。
	// condition 为空字符串时默认返回 true。
	Evaluate(ctx context.Context, condition string, data map[string]any) (bool, error)
}

// IConditionValidator 是求值器的可选扩展：在保存流程定义时预先校验条件表达式语法。
//
// 求值器实现该接口后，Engine.SaveDefinition 会对每条出边条件做一次静态校验，
// 把"表达式写错"从运行期推进失败前移到定义保存失败。
type IConditionValidator interface {
	// ValidateCondition 校验条件表达式语法；空字符串视为合法。
	ValidateCondition(condition string) error
}

// DefaultConditionEvaluator 提供零外部依赖的标准条件求值器。
//
// 支持的语法：
//   - 字面量 true / false
//   - 单标识符真值判断，例如 approved、data.approved
//   - 二元比较 <标识符> <操作符> <字面量>，操作符为 == != >= <= > <
//
// 右值可以是数字、布尔或带引号字符串；不支持 AND/OR、括号与嵌套表达式，
// 需要更强表达能力时通过 Engine.WithEvaluator 注入自定义实现。
type DefaultConditionEvaluator struct{}

// NewDefaultConditionEvaluator 创建默认条件求值器。
func NewDefaultConditionEvaluator() *DefaultConditionEvaluator {
	return &DefaultConditionEvaluator{}
}

// Evaluate 评估条件表达式。
func (e *DefaultConditionEvaluator) Evaluate(ctx context.Context, condition string, data map[string]any) (bool, error) {
	cond := strings.TrimSpace(condition)
	if cond == "" {
		return true, nil
	}

	if cond == "true" {
		return true, nil
	}
	if cond == "false" {
		return false, nil
	}

	if leftKey, op, rightValStr, ok := splitCondition(cond); ok {
		var leftVal any
		if data != nil {
			leftVal = data[leftKey]
		}
		return compareValues(leftVal, op, rightValStr)
	}

	// 单标识符探测
	ident := strings.TrimPrefix(cond, "data.")
	if data != nil {
		if val, ok := data[ident]; ok {
			switch v := val.(type) {
			case bool:
				return v, nil
			case nil:
				return false, nil
			case string:
				return v != "", nil
			case int, int8, int16, int32, int64:
				return fmt.Sprintf("%v", v) != "0", nil
			case float32, float64:
				return fmt.Sprintf("%v", v) != "0", nil
			default:
				return true, nil
			}
		}
	}

	return false, nil
}

// ValidateCondition 静态校验条件表达式语法。
func (e *DefaultConditionEvaluator) ValidateCondition(condition string) error {
	cond := strings.TrimSpace(condition)
	if cond == "" || cond == "true" || cond == "false" {
		return nil
	}

	left, _, right, ok := splitCondition(cond)
	if !ok {
		// 单标识符形式：不允许残留操作符字符，否则多半是写错的比较表达式。
		if strings.ContainsAny(cond, "=<>!") {
			return errors.NewCode(errors.InvalidInput, "workflow condition is not a valid expression").
				WithContext("condition", condition)
		}
		return nil
	}
	if left == "" {
		return errors.NewCode(errors.InvalidInput, "workflow condition has empty left operand").
			WithContext("condition", condition)
	}
	if right == "" {
		return errors.NewCode(errors.InvalidInput, "workflow condition has empty right operand").
			WithContext("condition", condition)
	}
	if strings.ContainsAny(left, "=<>!") {
		return errors.NewCode(errors.InvalidInput, "workflow condition left operand is not an identifier").
			WithContext("condition", condition).
			WithContext("left", left)
	}
	// 右值只能是字面量。带引号的字符串可以合法包含操作符字符（"a>=b"），
	// 裸字面量残留操作符字符则说明表达式本身写错了（例如 "amount >>= 1000"）。
	if !isQuotedLiteral(right) && strings.ContainsAny(right, "=<>!") {
		return errors.NewCode(errors.InvalidInput, "workflow condition right operand is not a literal").
			WithContext("condition", condition).
			WithContext("right", right)
	}
	return nil
}

// isQuotedLiteral 判断字面量是否被完整的同类引号包裹。
func isQuotedLiteral(s string) bool {
	if len(s) < 2 {
		return false
	}
	first, last := s[0], s[len(s)-1]
	return (first == '\'' || first == '"') && first == last
}

// conditionOperators 按长度降序排列，确保 ">=" 先于 ">" 被匹配。
var conditionOperators = []string{"==", "!=", ">=", "<=", ">", "<"}

// splitCondition 在引号之外定位第一个比较操作符，并切分出左值键、操作符与右值字面量。
//
// 逐字符扫描而不是直接 strings.Contains，是为了避免字符串字面量里的操作符字符
// （例如 name == "a>=b"）把表达式切错位置。
func splitCondition(cond string) (leftKey string, op string, rightValue string, ok bool) {
	var quote byte
	for i := 0; i < len(cond); i++ {
		ch := cond[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		for _, candidate := range conditionOperators {
			if len(cond)-i < len(candidate) {
				continue
			}
			if cond[i:i+len(candidate)] != candidate {
				continue
			}
			left := strings.TrimSpace(cond[:i])
			right := strings.TrimSpace(cond[i+len(candidate):])
			return strings.TrimPrefix(left, "data."), candidate, right, true
		}
	}
	return "", "", "", false
}

func compareValues(left any, op string, rightStr string) (bool, error) {
	rightStr = unquoteString(rightStr)

	// 布尔比较
	if rightBool, err := strconv.ParseBool(rightStr); err == nil {
		if leftBool, ok := left.(bool); ok {
			switch op {
			case "==":
				return leftBool == rightBool, nil
			case "!=":
				return leftBool != rightBool, nil
			default:
				return false, errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported operator %s for boolean", op))
			}
		}
	}

	// 数值比较
	if rightNum, err := strconv.ParseFloat(rightStr, 64); err == nil {
		if leftNum, ok := toFloat64(left); ok {
			switch op {
			case "==":
				return leftNum == rightNum, nil
			case "!=":
				return leftNum != rightNum, nil
			case ">":
				return leftNum > rightNum, nil
			case ">=":
				return leftNum >= rightNum, nil
			case "<":
				return leftNum < rightNum, nil
			case "<=":
				return leftNum <= rightNum, nil
			default:
				return false, errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported operator %s for number", op))
			}
		}
	}

	// 字符串比较
	leftStr := ""
	if left != nil {
		leftStr = fmt.Sprintf("%v", left)
	}
	switch op {
	case "==":
		return leftStr == rightStr, nil
	case "!=":
		return leftStr != rightStr, nil
	default:
		return false, errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported operator %s for string", op))
	}
}

func unquoteString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func toFloat64(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
