package impl

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

// BenchmarkTracer_NewEntryStartEnd 完整 trace 周期（单 goroutine）
func BenchmarkTracer_NewEntryStartEnd(b *testing.B) {
	t := NewMemoryTracer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.NewEntry("bench_case").Start().End(0, "")
	}
}

// BenchmarkTracer_NewEntryStartEnd_Parallel 并发完整 trace 周期
func BenchmarkTracer_NewEntryStartEnd_Parallel(b *testing.B) {
	t := NewMemoryTracer()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			t.NewEntry("bench_case").Start().End(0, "")
		}
	})
}

// BenchmarkTracer_MultiName_Parallel 多 name 并发（模拟多 case 同时采集）
func BenchmarkTracer_MultiName_Parallel(b *testing.B) {
	t := NewMemoryTracer()
	names := make([]string, 64)
	for i := range names {
		names[i] = "case_" + strconv.Itoa(i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			t.NewEntry(names[idx%len(names)]).Start().End(0, "")
			idx++
		}
	})
}

// BenchmarkTracer_FlushUnderLoad Flush 延迟（后台持续写入时）
func BenchmarkTracer_FlushUnderLoad(b *testing.B) {
	t := NewMemoryTracer()
	// 预热：填充一些数据
	for i := 0; i < 1000; i++ {
		t.NewEntry("warmup").Start().End(0, "")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 每次 flush 前写入少量数据模拟持续负载
		for j := 0; j < 10; j++ {
			t.NewEntry("load").Start().End(0, "")
		}
		t.Flush()
	}
}

// BenchmarkTracer_ShardScaling 不同 shard 数对并发吞吐量的影响
func BenchmarkTracer_ShardScaling(b *testing.B) {
	for _, shards := range []int{1, 2, 4, 8, 16, 32} {
		b.Run(fmt.Sprintf("shards=%d", shards), func(b *testing.B) {
			t := NewMemoryTracerWithShards(shards)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					t.NewEntry("bench").Start().End(0, "")
				}
			})
		})
	}
}

// BenchmarkTracer_Contention 不同并发度下的争用情况
func BenchmarkTracer_Contention(b *testing.B) {
	for _, workers := range []int{1, 4, 16, 64, 256} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			t := NewMemoryTracer()
			perWorker := b.N / workers
			if perWorker == 0 {
				perWorker = 1
			}
			var wg sync.WaitGroup
			b.ResetTimer()
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < perWorker; j++ {
						t.NewEntry("bench").Start().End(0, "")
					}
				}()
			}
			wg.Wait()
		})
	}
}
