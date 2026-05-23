// Package datecode 提供日期编码生成器。
//
// 支持生成 YYYYMMDD + 序列号/随机数 格式的编码，常用于订单号、流水号等业务场景。
package datecode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	"gochen/errors"
)

// Format 表示日期部分的格式模板。
type Format string

const (
	// FormatYYYYMMDD 生成 `YYYYMMDD` 形式的日期片段。
	FormatYYYYMMDD Format = "20060102" // 20250114
	// FormatYYYYMMDDHH 生成 `YYYYMMDDHH` 形式的日期片段。
	FormatYYYYMMDDHH Format = "2006010215" // 2025011409
	// FormatYYYYMMDDHHMM 生成 `YYYYMMDDHHMM` 形式的日期片段。
	FormatYYYYMMDDHHMM Format = "200601021504" // 202501140930
	// FormatYYYYMMDDHHMMSS 生成 `YYYYMMDDHHMMSS` 形式的日期片段。
	FormatYYYYMMDDHHMMSS Format = "20060102150405" // 20250114093045
	// FormatYYMMDD 生成 `YYMMDD` 形式的日期片段。
	FormatYYMMDD Format = "060102" // 250114
	// FormatMMDD 生成 `MMDD` 形式的日期片段。
	FormatMMDD Format = "0102" // 0114
)

// SuffixType 后缀类型。
type SuffixType int

const (
	// SuffixRandom 使用随机数作为后缀。
	SuffixRandom SuffixType = iota
	// SuffixSequence 使用递增序列号作为后缀（同一日期内递增）。
	SuffixSequence
)

// Option 生成器配置选项。
type Option func(*Generator)

// Generator 日期编码生成器。
type Generator struct {
	mux          sync.Mutex
	format       Format
	suffixType   SuffixType
	suffixDigits int
	prefix       string
	separator    string
	location     *time.Location

	// 序列号模式使用
	lastDate string
	sequence int64
	maxSeq   int64
}

// WithFormat 指定编码前缀里使用的日期格式。
func WithFormat(f Format) Option {
	return func(g *Generator) {
		g.format = f
	}
}

// WithSuffixDigits 设置随机数或序列号后缀的位数。
func WithSuffixDigits(digits int) Option {
	return func(g *Generator) {
		if digits > 0 && digits <= 18 {
			g.suffixDigits = digits
		}
	}
}

// WithSuffixType 指定后缀使用随机数还是递增序列。
func WithSuffixType(t SuffixType) Option {
	return func(g *Generator) {
		g.suffixType = t
	}
}

// WithPrefix 为编码添加固定业务前缀，例如 `ORD` 或 `TXN`。
func WithPrefix(prefix string) Option {
	return func(g *Generator) {
		g.prefix = prefix
	}
}

// WithSeparator 设置日期部分与后缀之间的分隔符。
func WithSeparator(sep string) Option {
	return func(g *Generator) {
		g.separator = sep
	}
}

// WithLocation 指定生成编码时使用的时区。
func WithLocation(loc *time.Location) Option {
	return func(g *Generator) {
		if loc != nil {
			g.location = loc
		}
	}
}

// NewGenerator 创建一个日期编码生成器，并应用可选配置。
func NewGenerator(opts ...Option) *Generator {
	g := &Generator{
		format:       FormatYYYYMMDD,
		suffixType:   SuffixRandom,
		suffixDigits: 6,
		location:     time.Local,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	g.maxSeq = pow10(g.suffixDigits) - 1
	return g
}

// Next 生成下一条编码。
func (g *Generator) Next() (string, error) {
	g.mux.Lock()
	defer g.mux.Unlock()

	now := time.Now().In(g.location)
	dateStr := now.Format(string(g.format))

	var suffix string
	switch g.suffixType {
	case SuffixSequence:
		suffix = g.nextSequence(dateStr)
	default:
		var err error
		suffix, err = g.randomSuffix()
		if err != nil {
			return "", err
		}
	}

	return g.prefix + dateStr + g.separator + suffix, nil
}

// nextSequence 为同一日期窗口生成递增序列号。
func (g *Generator) nextSequence(dateStr string) string {
	if dateStr != g.lastDate {
		g.lastDate = dateStr
		g.sequence = 0
	}
	g.sequence++
	if g.sequence > g.maxSeq {
		g.sequence = 1
	}
	return fmt.Sprintf("%0*d", g.suffixDigits, g.sequence)
}

// randomSuffix 生成一个定长随机数字后缀。
func (g *Generator) randomSuffix() (string, error) {
	max := pow10(g.suffixDigits)
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return "", errors.NewCode(errors.Internal, "failed to generate random suffix").
			WithContext("cause", err.Error())
	}
	return fmt.Sprintf("%0*d", g.suffixDigits, n.Int64()), nil
}

// pow10 返回 10 的 n 次方。
func pow10(n int) int64 {
	result := int64(1)
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// 便捷构造函数。

// NewOrderCodeGenerator 创建一个适合订单号场景的日期序列生成器。
func NewOrderCodeGenerator() *Generator {
	return NewGenerator(
		WithPrefix("ORD"),
		WithFormat(FormatYYYYMMDD),
		WithSuffixDigits(8),
		WithSuffixType(SuffixSequence),
	)
}

// NewTransactionCodeGenerator 创建一个适合交易号场景的日期随机码生成器。
func NewTransactionCodeGenerator() *Generator {
	return NewGenerator(
		WithPrefix("TXN"),
		WithFormat(FormatYYYYMMDDHHMMSS),
		WithSuffixDigits(6),
		WithSuffixType(SuffixRandom),
	)
}

// NewSerialCodeGenerator 创建一个带业务前缀的通用日期序列生成器。
func NewSerialCodeGenerator(prefix string, digits int) *Generator {
	return NewGenerator(
		WithPrefix(prefix),
		WithFormat(FormatYYYYMMDD),
		WithSuffixDigits(digits),
		WithSuffixType(SuffixSequence),
	)
}
