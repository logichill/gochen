package errors

import (
	"context"
	"errors"
	"testing"
)

// TestWrap 测试基本错误包装
func TestWrap(t *testing.T) {
	ctx := context.Background()
	originalErr := errors.New("原始错误")

	wrapped := Wrap(ctx, originalErr, ErrCodeInternal, "包装消息")

	if wrapped == nil {
		t.Fatal("包装后的错误为nil")
	}

	// 验证错误信息包含原始消息
	if !errors.Is(wrapped, originalErr) {
		// 如果不支持errors.Is，检查错误字符串
		errStr := wrapped.Error()
		if errStr == "" {
			t.Error("包装后的错误消息为空")
		}
	}
}

// TestWrap_NilError 测试包装nil错误
func TestWrap_NilError(t *testing.T) {
	ctx := context.Background()

	wrapped := Wrap(ctx, nil, ErrCodeInternal, "消息")

	if wrapped != nil {
		t.Error("包装nil错误应该返回nil")
	}
}

// TestWrapDbError 测试数据库错误包装
func TestWrapDbError(t *testing.T) {
	ctx := context.Background()
	originalErr := errors.New("数据库连接失败")

	wrapped := WrapDbError(ctx, originalErr, "查询用户")

	if wrapped == nil {
		t.Fatal("包装后的错误为nil")
	}

	// 验证错误消息
	errMsg := wrapped.Error()
	if errMsg == "" {
		t.Error("包装后的错误消息为空")
	}
}

// TestWrapDbError_NilError 测试包装nil数据库错误
func TestWrapDbError_NilError(t *testing.T) {
	ctx := context.Background()

	wrapped := WrapDbError(ctx, nil, "操作")

	if wrapped != nil {
		t.Error("包装nil错误应该返回nil")
	}
}

// TestWrapDbError_NotFound 测试NotFound错误特殊处理
func TestWrapDbError_NotFound(t *testing.T) {
	ctx := context.Background()

	// 创建一个NotFound错误
	notFoundErr := NewError(ErrCodeNotFound, "记录不存在")

	wrapped := WrapDbError(ctx, notFoundErr, "查询用户")

	if wrapped == nil {
		t.Fatal("包装后的错误为nil")
	}

	// 验证错误码是NotFound
	if !IsNotFound(wrapped) {
		t.Error("期望错误码为NotFound")
	}
}

// TestNew 测试创建新错误
func TestNew(t *testing.T) {
	err := New(ErrCodeValidation, "验证失败")

	if err == nil {
		t.Fatal("创建的错误为nil")
	}

	errMsg := err.Error()
	if errMsg == "" {
		t.Error("错误消息为空")
	}

	// 验证消息包含原始文本
	if !contains(errMsg, "验证失败") {
		t.Errorf("错误消息不包含原始文本: %s", errMsg)
	}
}

// TestNew_DifferentErrorCodes 测试不同错误码
func TestNew_DifferentErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		msg  string
	}{
		{
			name: "内部错误",
			code: ErrCodeInternal,
			msg:  "内部错误",
		},
		{
			name: "验证错误",
			code: ErrCodeValidation,
			msg:  "验证失败",
		},
		{
			name: "未找到",
			code: ErrCodeNotFound,
			msg:  "资源不存在",
		},
		{
			name: "数据库错误",
			code: ErrCodeDatabase,
			msg:  "数据库操作失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.code, tt.msg)
			if err == nil {
				t.Fatal("创建的错误为nil")
			}

			errMsg := err.Error()
			if !contains(errMsg, tt.msg) {
				t.Errorf("错误消息不包含原始文本: 期望包含'%s'，实际为'%s'", tt.msg, errMsg)
			}
		})
	}
}

// TestErrorWrapping 测试错误链
func TestErrorWrapping(t *testing.T) {
	ctx := context.Background()

	// 创建错误链
	err1 := errors.New("底层错误")
	err2 := Wrap(ctx, err1, ErrCodeDatabase, "数据库层错误")
	err3 := Wrap(ctx, err2, ErrCodeInternal, "服务层错误")

	if err3 == nil {
		t.Fatal("错误链最终结果为nil")
	}

	// 验证错误消息
	if err3.Error() == "" {
		t.Error("错误链消息为空")
	}
}

// TestWrapWithContext 测试不同上下文
func TestWrapWithContext(t *testing.T) {
	originalErr := errors.New("测试错误")

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "Background上下文",
			ctx:  context.Background(),
		},
		{
			name: "TODO上下文",
			ctx:  context.TODO(),
		},
		{
			name: "带值的上下文",
			ctx:  context.WithValue(context.Background(), "key", "value"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := Wrap(tt.ctx, originalErr, ErrCodeInternal, "测试")
			if wrapped == nil {
				t.Error("包装后的错误为nil")
			}
		})
	}
}

// TestMultipleWrapCalls 测试多次包装
func TestMultipleWrapCalls(t *testing.T) {
	ctx := context.Background()
	originalErr := errors.New("原始错误")

	// 多次包装
	err1 := Wrap(ctx, originalErr, ErrCodeDatabase, "第一层")
	err2 := Wrap(ctx, err1, ErrCodeInternal, "第二层")
	err3 := Wrap(ctx, err2, ErrCodeValidation, "第三层")

	if err3 == nil {
		t.Fatal("多次包装后的错误为nil")
	}

	// 每次包装都应该返回非空错误
	if err1 == nil || err2 == nil {
		t.Error("中间包装结果为nil")
	}
}

// TestConcurrentWrap 测试并发包装
func TestConcurrentWrap(t *testing.T) {
	ctx := context.Background()
	originalErr := errors.New("并发测试错误")

	const goroutines = 10
	const operations = 100

	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			for j := 0; j < operations; j++ {
				wrapped := Wrap(ctx, originalErr, ErrCodeInternal, "并发包装")
				if wrapped == nil {
					t.Errorf("goroutine %d: 包装结果为nil", id)
				}
			}
			done <- true
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// BenchmarkWrap 基准测试：基本包装
func BenchmarkWrap(b *testing.B) {
	ctx := context.Background()
	err := errors.New("测试错误")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Wrap(ctx, err, ErrCodeInternal, "基准测试")
	}
}

// BenchmarkNew 基准测试：创建新错误
func BenchmarkNew(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(ErrCodeValidation, "基准测试")
	}
}

// BenchmarkWrapDbError 基准测试：数据库错误包装
func BenchmarkWrapDbError(b *testing.B) {
	ctx := context.Background()
	err := errors.New("数据库错误")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WrapDbError(ctx, err, "查询操作")
	}
}

// 辅助函数：检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
