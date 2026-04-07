package atsf4g_go_robot_case

import (
	"fmt"
	"sync"
	"testing"
)

// BenchmarkQPSControllerAcquire_Unlimited 不限速场景
func BenchmarkQPSControllerAcquire_Unlimited(b *testing.B) {
	q := NewQPSController(0)
	defer q.Stop()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Acquire()
	}
}

// BenchmarkQPSControllerAcquire_HighQPS 极高 QPS（refiller 充分提供令牌）
func BenchmarkQPSControllerAcquire_HighQPS(b *testing.B) {
	q := NewQPSController(1e8)
	defer q.Stop()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Acquire()
	}
}

// BenchmarkQPSControllerAcquire_Parallel 多 goroutine 并发 Acquire
func BenchmarkQPSControllerAcquire_Parallel(b *testing.B) {
	q := NewQPSController(1e8)
	defer q.Stop()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.Acquire()
		}
	})
}

// BenchmarkQPSControllerSetQPS_Parallel 并发 SetQPS + Acquire
func BenchmarkQPSControllerSetQPS_Parallel(b *testing.B) {
	q := NewQPSController(1e6)
	defer q.Stop()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%100 == 0 {
				q.SetQPS(float64(1e6 + i%1000))
			} else {
				q.Acquire()
			}
			i++
		}
	})
}

// BenchmarkQPSControllerCurrentQPS 读取 CurrentQPS 的开销
func BenchmarkQPSControllerCurrentQPS(b *testing.B) {
	q := NewQPSController(100000)
	defer q.Stop()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.CurrentQPS()
	}
}

// BenchmarkQPSControllerContention 模拟真实场景：N 个 goroutine 争抢令牌
func BenchmarkQPSControllerContention(b *testing.B) {
	for _, workers := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			q := NewQPSController(1e8)
			defer q.Stop()
			var wg sync.WaitGroup
			perWorker := b.N / workers
			if perWorker == 0 {
				perWorker = 1
			}
			b.ResetTimer()
			for w := 0; w < workers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < perWorker; j++ {
						q.Acquire()
					}
				}()
			}
			wg.Wait()
		})
	}
}
