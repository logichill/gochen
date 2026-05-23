package snowflake

import (
	"sync"
	"testing"
	"time"
)

// TestNewGenerator 验证 NewGenerator。
func TestNewGenerator(t *testing.T) {
	tests := []struct {
		name         string
		datacenterID int64
		workerID     int64
		expectError  bool
	}{
		{
			name:         "有效的datacenterID和workerID",
			datacenterID: 1,
			workerID:     1,
			expectError:  false,
		},
		{
			name:         "datacenterID超出范围-负数",
			datacenterID: -1,
			workerID:     1,
			expectError:  true,
		},
		{
			name:         "datacenterID超出范围-超过最大值",
			datacenterID: 32,
			workerID:     1,
			expectError:  true,
		},
		{
			name:         "workerID超出范围-负数",
			datacenterID: 1,
			workerID:     -1,
			expectError:  true,
		},
		{
			name:         "workerID超出范围-超过最大值",
			datacenterID: 1,
			workerID:     32,
			expectError:  true,
		},
		{
			name:         "边界值-最大datacenterID和workerID",
			datacenterID: 31,
			workerID:     31,
			expectError:  false,
		},
		{
			name:         "边界值-最小datacenterID和workerID",
			datacenterID: 0,
			workerID:     0,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewGenerator(tt.datacenterID, tt.workerID)
			if tt.expectError {
				if err == nil {
					t.Errorf("期望错误但没有返回错误")
				}
			} else {
				if err != nil {
					t.Errorf("不期望错误但返回了错误: %v", err)
				}
				if gen == nil {
					t.Fatalf("生成器为nil")
				}
				if gen.datacenterID != tt.datacenterID {
					t.Errorf("datacenterID = %d, 期望 %d", gen.datacenterID, tt.datacenterID)
				}
				if gen.workerID != tt.workerID {
					t.Errorf("workerID = %d, 期望 %d", gen.workerID, tt.workerID)
				}
			}
		})
	}
}

// TestNextID_Uniqueness 验证 NextID Uniqueness。
func TestNextID_Uniqueness(t *testing.T) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		t.Fatalf("创建生成器失败: %v", err)
	}

	const count = 10000
	ids := make(map[int64]bool, count)

	for i := 0; i < count; i++ {
		id, err := gen.NextID()
		if err != nil {
			t.Fatalf("生成ID失败: %v", err)
		}

		if ids[id] {
			t.Fatalf("生成了重复的ID: %d", id)
		}
		ids[id] = true
	}

	if len(ids) != count {
		t.Errorf("生成的唯一ID数量 = %d, 期望 %d", len(ids), count)
	}
}

// TestNextID_Concurrent 验证 NextID Concurrent。
func TestNextID_Concurrent(t *testing.T) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		t.Fatalf("创建生成器失败: %v", err)
	}

	const goroutines = 10
	const idsPerGoroutine = 1000
	const totalIDs = goroutines * idsPerGoroutine

	var wg sync.WaitGroup
	idChan := make(chan int64, totalIDs)

	// 启动多个goroutine并发生成ID
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id, err := gen.NextID()
				if err != nil {
					t.Errorf("生成ID失败: %v", err)
					return
				}
				idChan <- id
			}
		}()
	}

	wg.Wait()
	close(idChan)

	// 检查唯一性
	ids := make(map[int64]bool, totalIDs)
	for id := range idChan {
		if ids[id] {
			t.Fatalf("并发环境下生成了重复的ID: %d", id)
		}
		ids[id] = true
	}

	if len(ids) != totalIDs {
		t.Errorf("生成的唯一ID数量 = %d, 期望 %d", len(ids), totalIDs)
	}
}

// TestNextID_TimestampMonotonic 验证 NextID TimestampMonotonic。
func TestNextID_TimestampMonotonic(t *testing.T) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		t.Fatalf("创建生成器失败: %v", err)
	}

	const count = 1000
	var prevTimestamp int64

	for i := 0; i < count; i++ {
		id, err := gen.NextID()
		if err != nil {
			t.Fatalf("生成ID失败: %v", err)
		}

		parsed := Parse(id)
		timestamp := parsed["timestamp"]

		if i > 0 && timestamp < prevTimestamp {
			t.Errorf("时间戳不是单调递增: 当前=%d, 上一个=%d", timestamp, prevTimestamp)
		}

		prevTimestamp = timestamp

		// 适当延迟以确保时间变化
		if i%100 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
}

// TestParse 验证 Parse。
func TestParse(t *testing.T) {
	gen, err := NewGenerator(10, 20)
	if err != nil {
		t.Fatalf("创建生成器失败: %v", err)
	}

	id, err := gen.NextID()
	if err != nil {
		t.Fatalf("生成ID失败: %v", err)
	}

	parsed := Parse(id)

	if parsed["datacenterID"] != 10 {
		t.Errorf("datacenterID = %d, 期望 10", parsed["datacenterID"])
	}

	if parsed["workerID"] != 20 {
		t.Errorf("workerID = %d, 期望 20", parsed["workerID"])
	}

	// 时间戳应该合理（在当前时间附近）
	now := time.Now().UnixNano() / 1e6
	if parsed["timestamp"] < now-1000 || parsed["timestamp"] > now+1000 {
		t.Errorf("时间戳异常: %d, 当前时间: %d", parsed["timestamp"], now)
	}

	// 序列号应该在有效范围内
	if parsed["sequence"] < 0 || parsed["sequence"] > maxSequence {
		t.Errorf("序列号超出范围: %d", parsed["sequence"])
	}
}

// TestDefaultGenerator 验证 DefaultGenerator。
func TestDefaultGenerator(t *testing.T) {
	// 测试默认生成器
	id1, err := NextID()
	if err != nil {
		t.Fatalf("使用默认生成器生成ID失败: %v", err)
	}

	id2, err := NextID()
	if err != nil {
		t.Fatalf("使用默认生成器生成ID失败: %v", err)
	}

	if id1 == id2 {
		t.Errorf("默认生成器生成了重复的ID")
	}
}

// TestSetDefaultGenerator 验证 SetDefaultGenerator。
func TestSetDefaultGenerator(t *testing.T) {
	// 保存原默认生成器的datacenterID和workerID
	originalID, _ := NextID()
	originalParsed := Parse(originalID)

	// 设置新的默认生成器
	err := SetDefaultGenerator(5, 10)
	if err != nil {
		t.Fatalf("设置默认生成器失败: %v", err)
	}

	// 验证新生成器的配置
	id, err := NextID()
	if err != nil {
		t.Fatalf("使用新默认生成器生成ID失败: %v", err)
	}

	parsed := Parse(id)
	if parsed["datacenterID"] != 5 {
		t.Errorf("datacenterID = %d, 期望 5", parsed["datacenterID"])
	}
	if parsed["workerID"] != 10 {
		t.Errorf("workerID = %d, 期望 10", parsed["workerID"])
	}

	// 恢复原默认生成器
	SetDefaultGenerator(originalParsed["datacenterID"], originalParsed["workerID"])
}

// TestSequenceOverflow 验证 SequenceOverflow。
func TestSequenceOverflow(t *testing.T) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		t.Fatalf("创建生成器失败: %v", err)
	}

	// 在极短时间内生成大量ID，触发序列号溢出
	const count = 5000
	ids := make([]int64, count)

	for i := 0; i < count; i++ {
		id, err := gen.NextID()
		if err != nil {
			t.Fatalf("生成ID失败: %v", err)
		}
		ids[i] = id
	}

	// 验证所有ID唯一
	idSet := make(map[int64]bool)
	for _, id := range ids {
		if idSet[id] {
			t.Fatalf("序列号溢出场景下生成了重复的ID: %d", id)
		}
		idSet[id] = true
	}
}

// BenchmarkNextID 用于评估 NextID 的性能。
func BenchmarkNextID(b *testing.B) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		b.Fatalf("创建生成器失败: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := gen.NextID()
		if err != nil {
			b.Fatalf("生成ID失败: %v", err)
		}
	}
}

// BenchmarkNextID_Parallel 用于评估 NextID Parallel 的性能。
func BenchmarkNextID_Parallel(b *testing.B) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		b.Fatalf("创建生成器失败: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := gen.NextID()
			if err != nil {
				b.Fatalf("生成ID失败: %v", err)
			}
		}
	})
}

// BenchmarkParse 用于评估 Parse 的性能。
func BenchmarkParse(b *testing.B) {
	gen, err := NewGenerator(1, 1)
	if err != nil {
		b.Fatalf("创建生成器失败: %v", err)
	}

	id, _ := gen.NextID()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Parse(id)
	}
}
