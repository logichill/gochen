// Package casecodec 提供命名风格的编解码。
//
// 与兄弟包一致，本包按 codec.ICodec 建模：
//
//	idcodec    ID   ⟷ any
//	jsoncodec  T    ⟷ []byte
//	casecodec  Words ⟷ string
//
// 关键设计：**引入规范中间表示 Words，而不是在格式之间两两转换**。
//
// 两两建模需要 N×(N-1)/2 个 codec，每加一种风格要新增 N 个；更糟的是其中
// 大部分组合不可逆（凡涉及 snake / kebab 的都会丢掉大小写与缩写边界），
// 只有 camel⟷pascal 这类特例才是双射。以 Words 为枢纽后：
//
//   - N 种风格只需 N 个 codec，转换靠组合：Pascal.Encode(Snake.Decode(s))；
//   - 信息丢失被收敛到 Decode（解析）这一处，而不是散落在 N² 个组合里；
//   - 两端类型不同（Words / string），ICodec 的抽象才真正携带信息。
//
// # 往返语义（重要）
//
// Decode 是**风格无关**的：它按大小写跃迁与分隔符切词，因此能接受
// snake / kebab / camel / pascal 等任意常见输入。这带来一条必须明说的性质：
//
//	Decode(Encode(w)) == w      ✔ 恒成立（词元已规范化为小写）
//	Encode(Decode(s)) == s      ✔ 仅当 s 已是该风格的规范形式
//
// 反例：Pascal.Encode(Decode("UserID")) == "UserId"。缩写 ID 在解析成词元
// ["user","id"] 时已丢失边界信息，渲染侧无从还原。需要保留缩写形态的场景，
// 应显式维护缩写表，而不是指望往返。
package casecodec

import (
	"strings"
	"unicode"

	"gochen/codec"
	"gochen/errors"
)

// Words 是命名的规范中间表示：已切分、已小写的词元序列。
//
// 之所以统一小写：Words 的职责是表达"由哪几个词构成"，
// 大小写属于渲染决策，归 Encode 管。
type Words []string

// ICodec 是命名风格 codec 的统一契约。
type ICodec = codec.ICodec[Words, string]

// 各风格 codec。它们都是无状态值，可直接复用。
var (
	// Snake 渲染 snake_case，是 ORM 列名的默认风格。
	Snake ICodec = snakeCodec{}
	// Kebab 渲染 kebab-case。
	Kebab ICodec = kebabCodec{}
	// Camel 渲染 camelCase。
	Camel ICodec = camelCodec{}
	// Pascal 渲染 PascalCase。
	Pascal ICodec = pascalCodec{}
	// ScreamingSnake 渲染 SCREAMING_SNAKE_CASE，常用于环境变量与常量。
	ScreamingSnake ICodec = screamingSnakeCodec{}
)

// Convert 把任意常见风格的命名转换为目标风格。
//
// 等价于 target.Encode(Parse(in))，是最常用的组合形态。
func Convert(target ICodec, in string) (string, error) {
	if target == nil {
		return "", errors.NewCode(errors.InvalidInput, "target case codec is nil")
	}
	return target.Encode(Parse(in))
}

// --- 解析：风格无关的切词 ---

// Parse 把任意常见风格的命名切分为规范词元。
//
// 切词规则（与历史 textcase.Snake 的边界判定保持一致）：
//   - `_` 与 `-` 是显式分隔符；
//   - 小写/数字 后接 大写 → 断词（userID → user|id）；
//   - 连续大写后接小写 → 在最后一个大写前断词（HTTPServer → http|server）。
//
// 空输入返回 nil；连续分隔符不产生空词元。
func Parse(in string) Words {
	if in == "" {
		return nil
	}
	runes := []rune(in)
	var words Words
	var current []rune

	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = nil
		}
	}

	upperSeqLen := 0
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
			upperSeqLen = 0
		case unicode.IsUpper(r):
			if i > 0 && shouldBreakBeforeUpper(runes, i, upperSeqLen) {
				flush()
			}
			current = append(current, unicode.ToLower(r))
			upperSeqLen++
		default:
			upperSeqLen = 0
			current = append(current, unicode.ToLower(r))
		}
	}
	flush()
	return words
}

// shouldBreakBeforeUpper 判断位置 i 的大写字母前是否应断词。
//
// upperSeqLen 是紧邻其前的连续大写长度（遇非大写归零）。
func shouldBreakBeforeUpper(runes []rune, i int, upperSeqLen int) bool {
	prev := runes[i-1]
	if unicode.IsLower(prev) || unicode.IsDigit(prev) {
		return true
	}
	// 连续大写的尾字母若领起一个小写词，则它属于下一个词：
	// HTTPServer → http|server（S 前断开），而 ID 不断（无后继小写）。
	nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
	return unicode.IsUpper(prev) && nextLower && upperSeqLen > 1
}

// --- 渲染 ---

type snakeCodec struct{}

func (snakeCodec) Encode(w Words) (string, error) { return strings.Join(w, "_"), nil }
func (snakeCodec) Decode(raw string) (Words, error) {
	return Parse(raw), nil
}

type kebabCodec struct{}

func (kebabCodec) Encode(w Words) (string, error) { return strings.Join(w, "-"), nil }
func (kebabCodec) Decode(raw string) (Words, error) {
	return Parse(raw), nil
}

type screamingSnakeCodec struct{}

func (screamingSnakeCodec) Encode(w Words) (string, error) {
	return strings.ToUpper(strings.Join(w, "_")), nil
}
func (screamingSnakeCodec) Decode(raw string) (Words, error) {
	return Parse(raw), nil
}

type pascalCodec struct{}

func (pascalCodec) Encode(w Words) (string, error) {
	var b strings.Builder
	for _, word := range w {
		b.WriteString(title(word))
	}
	return b.String(), nil
}
func (pascalCodec) Decode(raw string) (Words, error) {
	return Parse(raw), nil
}

type camelCodec struct{}

func (camelCodec) Encode(w Words) (string, error) {
	var b strings.Builder
	for i, word := range w {
		if i == 0 {
			b.WriteString(word)
			continue
		}
		b.WriteString(title(word))
	}
	return b.String(), nil
}
func (camelCodec) Decode(raw string) (Words, error) {
	return Parse(raw), nil
}

// title 把词元首字母大写；词元已由 Parse 规范为小写。
func title(word string) string {
	if word == "" {
		return ""
	}
	runes := []rune(word)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
