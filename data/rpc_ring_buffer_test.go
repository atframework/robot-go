package atsf4g_go_robot_user

import (
	"sync"
	"sync/atomic"
	"testing"
)

// BenchmarkRPCRingBuffer_StoreLoadDelete 基准：Store → LoadAndDelete 配对
func BenchmarkRPCRingBuffer_StoreLoadDelete(b *testing.B) {
	rb := NewRPCRingBuffer(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq := uint64(i)
		_ = rb.Store(seq, nil)
		rb.LoadAndDelete(seq)
	}
}

// BenchmarkRPCRingBuffer_StoreLoadDelete_Parallel 并发 Store+LoadAndDelete
func BenchmarkRPCRingBuffer_StoreLoadDelete_Parallel(b *testing.B) {
	rb := NewRPCRingBuffer(65536)
	var seq atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := seq.Add(1)
			_ = rb.Store(s, nil)
			rb.LoadAndDelete(s)
		}
	})
}

// BenchmarkRPCRingBuffer_Store 仅测试 Store 写入
func BenchmarkRPCRingBuffer_Store(b *testing.B) {
	rb := NewRPCRingBuffer(65536)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rb.Store(uint64(i), nil)
	}
}

// BenchmarkRPCRingBuffer_LoadAndDelete_Miss 未命中的快路径（occupied=false）
func BenchmarkRPCRingBuffer_LoadAndDelete_Miss(b *testing.B) {
	rb := NewRPCRingBuffer(1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.LoadAndDelete(uint64(i))
	}
}

// BenchmarkRPCRingBuffer_VsSyncMap 与 sync.Map 对比基准
func BenchmarkRPCRingBuffer_VsSyncMap(b *testing.B) {
	b.Run("RingBuffer", func(b *testing.B) {
		rb := NewRPCRingBuffer(65536)
		var seq atomic.Uint64
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				s := seq.Add(1)
				_ = rb.Store(s, nil)
				rb.LoadAndDelete(s)
			}
		})
	})

	b.Run("SyncMap", func(b *testing.B) {
		var m sync.Map
		var seq atomic.Uint64
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				s := seq.Add(1)
				m.Store(s, (*TaskActionUser)(nil))
				m.LoadAndDelete(s)
			}
		})
	})
}
