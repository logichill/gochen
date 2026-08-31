// Package codec 提供通用编解码抽象。
package codec

// ICodec 定义业务值与原始载体之间的双向转换契约。
//
// 设计目标：
// - 让不同载体（如 `any`、`[]byte`）的 codec 可以共享统一抽象；
// - 由具体子包表达实现语义，各子包统一以 codec 形态提供能力：
//
//	idcodec    ID    ⟷ any      标识值与通用载体
//	jsoncodec  T     ⟷ []byte   JSON 文本
//	casecodec  Words ⟷ string   命名风格（snake / kebab / camel / pascal / screaming）
type ICodec[T any, Raw any] interface {
	Encode(v T) (Raw, error)
	Decode(raw Raw) (T, error)
}
