package atsf4g_go_robot_case

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	user_data "github.com/atframework/robot-go/data"
	report_impl "github.com/atframework/robot-go/report/impl"
)

// =============================================================================
// 模拟 Agent 整体运行流程的集成压测
//
// 覆盖热路径：
//   QPSController.Acquire → 多协程 dispatch → TaskActionManager.RunTaskAction
//   → HookRun（模拟用例逻辑）→ Tracer.NewEntry/Start/End → onFinish 回调链
// =============================================================================

// noop case：注册一个只做 Yield/Resume 的最小模拟用例
func init() {
	RegisterCase("__bench_noop", func(t *TaskActionCase, user *user_data.UserHolder, args []string) error {
		return nil
	}, 5)
}

// BenchmarkIntegration_AgentPipeline 端到端模拟 Agent 压测执行流程
// 参数矩阵：不同 userCount × batchCount 组合
func BenchmarkIntegration_AgentPipeline(b *testing.B) {
	configs := []struct {
		users   int64
		runTime int64
	}{
		{10000, 100},
	}

	for _, cfg := range configs {
		b.Run(fmt.Sprintf("users=%d_run=%d", cfg.users, cfg.runTime), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				runAgentPipeline(b, cfg.users, cfg.runTime)
			}
		})
	}
}

// BenchmarkIntegration_TracerPressureCombined Tracer + Pressure 联动
func BenchmarkIntegration_TracerPressureCombined(b *testing.B) {
	tracer := report_impl.NewMemoryTracer()
	pressure := report_impl.NewMemoryPressureController()
	pressure.SetTargetQPS(1e6)
	pressure.Start(100 * time.Millisecond)
	defer pressure.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pressure.AddPending()
			entry := tracer.NewEntry("combined_bench").Start()
			// simulate minimal work
			entry.End(0, "")
			pressure.DonePending()
		}
	})
	b.StopTimer()
	_ = tracer.Flush()
}

// BenchmarkIntegration_FullContext 完全模拟 RunCaseInner 但跳过网络
func BenchmarkIntegration_FullContext(b *testing.B) {
	for _, runTime := range []int64{1, 3} {
		b.Run(fmt.Sprintf("runTime=%d", runTime), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ctx := context.Background()
				tracer := report_impl.NewMemoryTracer()
				pressure := report_impl.NewMemoryPressureController()

				params := Params{
					CaseName:     "__bench_noop",
					OpenIDPrefix: "bench_",
					OpenIDStart:  0,
					OpenIDEnd:    500,
					TargetQPS:    0, // 不限速，测框架吞吐上限
					RunTime:      runTime,
				}
				_ = RunCaseInner(ctx, params, tracer, pressure, false, false)
			}
		})
	}
}

// BenchmarkIntegration_PerCoreThroughput 限制可用 CPU 数量，测量单核性能
// 使用 runtime.GOMAXPROCS 限制并行度，计算吞吐量 / CPU核数 得到单核吞吐
func BenchmarkIntegration_PerCoreThroughput(b *testing.B) {
	for _, maxProcs := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("GOMAXPROCS=%d", maxProcs), func(b *testing.B) {
			prev := runtime.GOMAXPROCS(maxProcs)
			defer runtime.GOMAXPROCS(prev)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runAgentPipeline(b, 10000, 1)
			}
			b.StopTimer()
			// 自定义指标：每核吞吐 = 10000 users / (ns_per_op * maxProcs)
			b.ReportMetric(float64(10000)/float64(maxProcs), "users/core/op")
		})
	}
}

// =============================================================================
// helper functions
// =============================================================================

// runAgentPipeline 模拟完整的 Agent 任务执行流水线（不经过网络）
func runAgentPipeline(_ testing.TB, userCount, runTime int64) {
	ctx := context.Background()
	tracer := report_impl.NewMemoryTracer()
	pressure := report_impl.NewMemoryPressureController()

	params := Params{
		CaseName:     "__bench_noop",
		OpenIDPrefix: "bench_",
		OpenIDStart:  0,
		OpenIDEnd:    userCount,
		TargetQPS:    0, // 不限速
		RunTime:      runTime,
	}
	_ = RunCaseInner(ctx, params, tracer, pressure, false, false)
}
