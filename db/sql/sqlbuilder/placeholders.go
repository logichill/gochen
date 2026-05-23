package sqlbuilder

import (
	"reflect"
	"strings"

	"gochen/errors"
)

// expandPlaceholders 将包含 slice/array 参数的占位符表达式展开为标准 (?, ?, ...) 形式。
//
// 说明：
// - 约定：
// - - cond 中 ? 的数量必须与 args 数量一致，否则返回 InvalidInput；
// - - 若对应参数是 slice/array（非字符串/非 []byte），会展开为多个 ? 并扁平化参数；
// - - 空 slice/array 将生成 "0=1" 以保证语义为“无匹配”，避免 SQL 语法错误。
func expandPlaceholders(cond string, args []any) (string, []any, error) {
	if cond == "" || len(args) == 0 {
		return cond, args, nil
	}

	for _, arg := range args {
		if values, ok := tryFlattenSliceArg(arg); ok && len(values) == 0 {
			// 空集合意味着无匹配，直接返回恒 false 条件以避免 SQL 语法错误。
			return "0=1", nil, nil
		}
	}

	var (
		builder strings.Builder
		flat    = make([]any, 0, len(args))
		argIdx  int
	)

	for i := 0; i < len(cond); i++ {
		ch := cond[i]
		if ch != '?' {
			builder.WriteByte(ch)
			continue
		}

		if argIdx >= len(args) {
			return "", nil, errors.NewCode(errors.InvalidInput, "sql: placeholder/args mismatch").
				WithContext("cond", cond).
				WithContext("arg_idx", argIdx).
				WithContext("args_len", len(args))
		}
		arg := args[argIdx]
		argIdx++

		if values, ok := tryFlattenSliceArg(arg); ok {
			if len(values) == 0 {
				builder.WriteString("0=1")
				continue
			}
			builder.WriteByte('(')
			for j := range values {
				if j > 0 {
					builder.WriteString(", ")
				}
				builder.WriteByte('?')
			}
			builder.WriteByte(')')
			flat = append(flat, values...)
			continue
		}

		builder.WriteByte('?')
		flat = append(flat, arg)
	}

	if argIdx != len(args) {
		return "", nil, errors.NewCode(errors.InvalidInput, "sql: placeholder/args mismatch").
			WithContext("cond", cond).
			WithContext("arg_idx", argIdx).
			WithContext("args_len", len(args))
	}

	return builder.String(), flat, nil
}

func tryFlattenSliceArg(arg any) ([]any, bool) {
	// 排除字符串与 []byte
	switch arg.(type) {
	case string, []byte:
		return nil, false
	}

	v := reflect.ValueOf(arg)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, false
	}

	l := v.Len()
	out := make([]any, 0, l)
	for i := 0; i < l; i++ {
		out = append(out, v.Index(i).Interface())
	}
	return out, true
}
