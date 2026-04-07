package impl

import (
	"testing"
	"time"
)

// BenchmarkPressure_AddDonePending 最热路径：atomic Add/Sub
func BenchmarkPressure_AddDonePending(b *testing.B) {
	p := NewMemoryPressureController()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.AddPending()
		p.DonePending()
	}
}

// BenchmarkPressure_AddDonePending_Parallel 并发 AddPending / DonePending
func BenchmarkPressure_AddDonePending_Parallel(b *testing.B) {
	p := NewMemoryPressureController()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.AddPending()
			p.DonePending()
		}
	})
}

// BenchmarkPressure_EffectiveQPS 读取 EffectiveQPS 的开销（含 mutex）
func BenchmarkPressure_EffectiveQPS(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(100000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.EffectiveQPS()
	}
}

// BenchmarkPressure_EffectiveQPS_Parallel 并发读取
func BenchmarkPressure_EffectiveQPS_Parallel(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(100000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = p.EffectiveQPS()
		}
	})
}

// BenchmarkPressure_Detect 单次 detect 延迟（含 runtime/metrics 读取）
func BenchmarkPressure_Detect(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(100000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.detect()
	}
}

// BenchmarkPressure_FlushSnapshots 快照 flush 开销
func BenchmarkPressure_FlushSnapshots(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(100000)
	// 先积累一些快照
	for i := 0; i < 100; i++ {
		p.detect()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.detect() // 产生一个快照
		_ = p.FlushSnapshots()
	}
}

// BenchmarkPressure_FullCycle 完整周期：Start → detect → Stop
func BenchmarkPressure_FullCycle(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewMemoryPressureController()
		p.SetTargetQPS(100000)
		p.Start(100 * time.Millisecond)
		p.detect()
		p.Stop()
	}
}
