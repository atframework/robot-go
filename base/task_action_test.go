package atsf4g_go_robot_protocol_base

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// benchTaskAction 是用于 benchmark 的最小 TaskActionImpl 实现
type benchTaskAction struct {
	TaskActionBase
}

func (b *benchTaskAction) HookRun() error {
	return nil
}

func (b *benchTaskAction) Log(format string, a ...any) {}

func newBenchTask() *benchTaskAction {
	t := &benchTaskAction{
		TaskActionBase: *NewTaskActionBase(0, "bench"),
	}
	t.Impl = t
	return t
}

// BenchmarkTaskActionManager_RunTaskAction 任务提交吞吐（无池）
func BenchmarkTaskActionManager_RunTaskAction_NoPool(b *testing.B) {
	mgr := NewTaskActionManager()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := newBenchTask()
		done := make(chan struct{}, 1)
		t.InitOnFinish(func(TaskActionImpl, error) { done <- struct{}{} })
		mgr.RunTaskAction(t)
		<-done
	}
}

// BenchmarkTaskActionManager_RunTaskAction_Pool 任务提交吞吐（ants 池）
func BenchmarkTaskActionManager_RunTaskAction_Pool(b *testing.B) {
	mgr := NewTaskActionManagerWithPool(256)
	defer mgr.ReleasePool()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := newBenchTask()
		done := make(chan struct{}, 1)
		t.InitOnFinish(func(TaskActionImpl, error) { done <- struct{}{} })
		mgr.RunTaskAction(t)
		<-done
	}
}

// BenchmarkTaskActionManager_RunTaskAction_Pool_Parallel 并发提交（ants 池）
func BenchmarkTaskActionManager_RunTaskAction_Pool_Parallel(b *testing.B) {
	mgr := NewTaskActionManagerWithPool(1024)
	defer mgr.ReleasePool()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			t := newBenchTask()
			done := make(chan struct{}, 1)
			t.InitOnFinish(func(TaskActionImpl, error) { done <- struct{}{} })
			mgr.RunTaskAction(t)
			<-done
		}
	})
}

// BenchmarkTaskActionManager_Scaling 不同池大小对吞吐的影响
func BenchmarkTaskActionManager_Scaling(b *testing.B) {
	for _, poolSize := range []int{16, 64, 256, 1024} {
		b.Run(fmt.Sprintf("pool=%d", poolSize), func(b *testing.B) {
			mgr := NewTaskActionManagerWithPool(poolSize)
			defer mgr.ReleasePool()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					t := newBenchTask()
					done := make(chan struct{}, 1)
					t.InitOnFinish(func(TaskActionImpl, error) { done <- struct{}{} })
					mgr.RunTaskAction(t)
					<-done
				}
			})
		})
	}
}

// BenchmarkYieldResume Yield + Resume 配对的 channel 往返开销
func BenchmarkYieldResume(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := newBenchTask()
		t.InitTaskId(uint64(i + 1))

		awaitData := TaskActionAwaitData{
			WaitingType: TaskActionAwaitTypeRPC,
			WaitingId:   uint64(i + 1),
		}
		resumeData := &TaskActionResumeData{Data: 42}

		// 先设置 AwaitData 以便 Resume 能匹配
		t.SetAwaitData(awaitData)
		go func() {
			t.Resume(&awaitData, resumeData)
		}()
		_ = t.Yield()
	}
}

// BenchmarkYieldResume_Parallel 并发 Yield/Resume
func BenchmarkYieldResume_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		id := uint64(1)
		for pb.Next() {
			t := newBenchTask()
			t.InitTaskId(id)
			awaitData := TaskActionAwaitData{
				WaitingType: TaskActionAwaitTypeRPC,
				WaitingId:   id,
			}
			t.SetAwaitData(awaitData)
			go func() {
				t.Resume(&awaitData, &TaskActionResumeData{Data: 42})
			}()
			_ = t.Yield()
			id++
		}
	})
}

// BenchmarkAllocTaskId 测试 taskId 分配的 atomic 开销
func BenchmarkAllocTaskId(b *testing.B) {
	mgr := NewTaskActionManager()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.allocTaskId()
	}
}

// BenchmarkAllocTaskId_Parallel 并发 taskId 分配
func BenchmarkAllocTaskId_Parallel(b *testing.B) {
	mgr := NewTaskActionManager()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = mgr.allocTaskId()
		}
	})
}

// BenchmarkFinishCallback 测试 Finish 回调链的开销
func BenchmarkFinishCallback(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := newBenchTask()
		t.InitTaskId(uint64(i + 1))
		var wg sync.WaitGroup
		wg.Add(1)
		t.InitOnFinish(func(TaskActionImpl, error) { wg.Done() })
		t.Finish(nil)
		wg.Wait()
	}
}

// BenchmarkTimeout_NoTimeout 无超时配置下 RunTaskAction 的开销
func BenchmarkTimeout_NoTimeout(b *testing.B) {
	mgr := NewTaskActionManagerWithPool(256)
	defer mgr.ReleasePool()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := &benchTaskAction{
			TaskActionBase: *NewTaskActionBase(0, "bench"), // timeout=0 不启动定时器
		}
		t.Impl = t
		done := make(chan struct{}, 1)
		t.InitOnFinish(func(TaskActionImpl, error) { done <- struct{}{} })
		mgr.RunTaskAction(t)
		<-done
	}
}

// BenchmarkTimeout_WithTimeout 带超时定时器下 RunTaskAction 的开销
func BenchmarkTimeout_WithTimeout(b *testing.B) {
	mgr := NewTaskActionManagerWithPool(256)
	defer mgr.ReleasePool()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := &benchTaskAction{
			TaskActionBase: *NewTaskActionBase(time.Second*30, "bench"),
		}
		t.Impl = t
		done := make(chan struct{}, 1)
		t.InitOnFinish(func(TaskActionImpl, error) { done <- struct{}{} })
		mgr.RunTaskAction(t)
		<-done
	}
}
