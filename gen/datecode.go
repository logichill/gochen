package gen

import "gochen/gen/datecode"

// NewDateCodeGenerator 创建日期编码生成器。
func NewDateCodeGenerator(opts ...datecode.Option) IGenerator[string] {
	return datecode.NewGenerator(opts...)
}

// NewOrderCodeGenerator 创建订单号日期序列生成器 (ORD + YYYYMMDD + 8位递增序列)。
func NewOrderCodeGenerator() IGenerator[string] {
	return datecode.NewOrderCodeGenerator()
}

// NewTransactionCodeGenerator 创建交易号日期随机码生成器 (TXN + YYYYMMDDHHMMSS + 6位随机数)。
func NewTransactionCodeGenerator() IGenerator[string] {
	return datecode.NewTransactionCodeGenerator()
}

// NewSerialCodeGenerator 创建带自定义前缀的通用日期序列生成器。
func NewSerialCodeGenerator(prefix string, digits int) IGenerator[string] {
	return datecode.NewSerialCodeGenerator(prefix, digits)
}
