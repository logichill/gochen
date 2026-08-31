package workflow

import (
	"context"
	"testing"

	"gochen/errors"
	"gochen/testkit/require"
)

func TestDefaultConditionEvaluator_Evaluate(t *testing.T) {
	ctx := context.Background()
	eval := NewDefaultConditionEvaluator()
	data := map[string]any{
		"amount":   500,
		"approved": true,
		"rejected": false,
		"channel":  "vip",
		"note":     "",
		"ratio":    1.5,
		"zero":     0,
		"label":    "a>=b",
	}

	cases := []struct {
		name string
		cond string
		want bool
	}{
		{"空条件默认为真", "", true},
		{"字面量 true", "true", true},
		{"字面量 false", "false", false},
		{"数值大于", "data.amount > 100", true},
		{"数值大于不成立", "data.amount > 1000", false},
		{"数值大于等于边界", "amount >= 500", true},
		{"数值小于", "amount < 500", false},
		{"数值相等", "amount == 500", true},
		{"数值不等", "amount != 500", false},
		{"浮点比较", "ratio > 1", true},
		{"布尔相等", "approved == true", true},
		{"布尔不等", "rejected != true", true},
		{"字符串双引号", `channel == "vip"`, true},
		{"字符串单引号", "channel == 'vip'", true},
		{"字符串不等", `channel != "std"`, true},
		{"单标识符布尔真", "approved", true},
		{"单标识符布尔假", "rejected", false},
		{"单标识符空字符串为假", "note", false},
		{"单标识符零值为假", "zero", false},
		{"缺失键为假", "missing_key", false},
		{"引号内操作符不参与切分", `label == "a>=b"`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eval.Evaluate(ctx, tc.cond, data)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDefaultConditionEvaluator_EvaluateNilData(t *testing.T) {
	ctx := context.Background()
	eval := NewDefaultConditionEvaluator()

	got, err := eval.Evaluate(ctx, "", nil)
	require.NoError(t, err)
	require.True(t, got)

	got, err = eval.Evaluate(ctx, "missing", nil)
	require.NoError(t, err)
	require.False(t, got)
}

func TestSplitCondition_QuoteAware(t *testing.T) {
	// 引号内的 ">=" 不能把表达式切在错误位置。
	left, op, right, ok := splitCondition(`name == "a>=b"`)
	require.True(t, ok)
	require.Equal(t, "name", left)
	require.Equal(t, "==", op)
	require.Equal(t, `"a>=b"`, right)

	// data. 前缀会被剥掉，两字符操作符优先于单字符。
	left, op, right, ok = splitCondition("data.amount >= 1000")
	require.True(t, ok)
	require.Equal(t, "amount", left)
	require.Equal(t, ">=", op)
	require.Equal(t, "1000", right)

	// 无操作符时返回 false，交给单标识符分支处理。
	_, _, _, ok = splitCondition("approved")
	require.False(t, ok)
}

func TestDefaultConditionEvaluator_ValidateCondition(t *testing.T) {
	eval := NewDefaultConditionEvaluator()

	valid := []string{
		"",
		"true",
		"false",
		"approved",
		"data.approved",
		"amount >= 1000",
		"data.amount < 100",
		`channel == "vip"`,
		"channel == 'vip'",
		`label == "a>=b"`,
	}
	for _, cond := range valid {
		require.NoErrorf(t, eval.ValidateCondition(cond), "condition should be valid: %s", cond)
	}

	invalid := []string{
		"amount >>= 1000", // 右值残留操作符
		">= 1000",         // 缺左值
		"amount >=",       // 缺右值
		"a == b == c",     // 右值残留操作符
		"amount ! 5",      // 非法操作符残留在单标识符分支
	}
	for _, cond := range invalid {
		err := eval.ValidateCondition(cond)
		require.Errorf(t, err, "condition should be invalid: %s", cond)
		require.Truef(t, errors.Is(err, errors.InvalidInput), "expect InvalidInput for: %s", cond)
	}
}

func TestEngine_SaveDefinitionRejectsInvalidCondition(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	def := &Definition{
		ID:          "bad-cond",
		StartNodeID: "gate",
		Nodes: []Node{
			{ID: "gate", Edges: []Edge{
				NewConditionalEdge("high", "amount >>= 1000"),
				NewDefaultEdge("low"),
			}},
			{ID: "high"},
			{ID: "low"},
		},
	}

	err := engine.SaveDefinition(ctx, def)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
	require.ErrorContains(t, err, "condition")
}
