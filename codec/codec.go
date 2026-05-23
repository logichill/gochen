// Package codec 提供通用编解码抽象。
package codec

// ICodec 定义业务值与原始载体之间的双向转换契约。
//
// 设计目标：
// - 让不同载体（如 `any`、`[]byte`）的 codec 可以共享统一抽象；
// - 由具体子包表达实现语义，例如 `idcodec` 处理标识值，`jsoncodec` 处理 JSON 文本。
type ICodec[T any, Raw any] interface {
	Encode(v T) (Raw, error)
	Decode(raw Raw) (T, error)
}
