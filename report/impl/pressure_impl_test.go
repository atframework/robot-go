package impl

import (
	"math"
	"testing"
	"time"

	"github.com/atframework/robot-go/report"
)

// ============================================================
// 辅助函数：模拟延迟并触发 N 轮 detect
// ============================================================

// feedAndDetect 向控制器注入 numSamples 条延迟样本后执行一轮 detect。
func feedAndDetect(p *MemoryPressureController, latency time.Duration, numSamples int) {
	for i := 0; i < numSamples; i++ {
		p.RecordLatency(latency)
	}
	p.detect()
}

// simulateService 模拟一个在 maxQPS 以下延迟恒定、超过后线性增长的服务。
// 返回给定 currentQPS 下的 P50 延迟。
func simulateLatency(baseLatency time.Duration, maxQPS, currentQPS float64) time.Duration {
	if currentQPS <= maxQPS || maxQPS <= 0 {
		return baseLatency
	}
	ratio := currentQPS / maxQPS
	return time.Duration(float64(baseLatency) * ratio)
}

// runAdaptive 运行 N 轮自适应探测，使用 simulateLatency 提供样本。
func runAdaptive(p *MemoryPressureController, baseLatency time.Duration, maxQPS float64, rounds int) {
	for i := 0; i < rounds; i++ {
		cur := p.EffectiveQPS()
		lat := simulateLatency(baseLatency, maxQPS, cur)
		samples := int(math.Max(cur/10, 10)) // 每轮至少 10 个样本
		if samples > 500 {
			samples = 500
		}
		feedAndDetect(p, lat, samples)
	}
}

// ============================================================
// 功能测试
// ============================================================

// TestAdaptive_BaselineCollection 验证基线收集阶段。
func TestAdaptive_BaselineCollection(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	if p.Phase() != phaseBaseline {
		t.Fatalf("初始阶段应为 baseline, got %s", p.Phase())
	}

	// 注入基线数据
	for i := 0; i < baselineWindowCount; i++ {
		feedAndDetect(p, 20*time.Millisecond, 50)
	}

	if !p.baselineSet {
		t.Fatal("基线应已设置")
	}
	if p.Phase() != phaseSlowStart {
		t.Fatalf("基线收集完成后应进入 slow_start, got %s", p.Phase())
	}
	// 基线 P50 应接近 20ms
	baseMs := p.BaselineP50Ns() / float64(time.Millisecond)
	if baseMs < 15 || baseMs > 25 {
		t.Fatalf("基线 P50 应约为 20ms, got %.2fms", baseMs)
	}
}

// TestAdaptive_SlowStartGrowth 验证慢启动阶段的指数增长。
func TestAdaptive_SlowStartGrowth(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	// 收集基线（低 QPS 无延迟增长）
	for i := 0; i < baselineWindowCount; i++ {
		feedAndDetect(p, 10*time.Millisecond, 50)
	}

	initialQPS := p.CurrentQPS()
	if p.Phase() != phaseSlowStart {
		t.Fatalf("expected slow_start, got %s", p.Phase())
	}

	// 5 轮低延迟：QPS 应该指数增长
	for i := 0; i < 5; i++ {
		feedAndDetect(p, 10*time.Millisecond, 50)
	}

	growthFactor := p.CurrentQPS() / initialQPS
	if growthFactor < 3 {
		t.Errorf("慢启动应有显著增长: initial=%.0f current=%.0f factor=%.2f",
			initialQPS, p.CurrentQPS(), growthFactor)
	}
}

// TestAdaptive_SlowStartToProbing 验证延迟上升时从慢启动切换到线性探测。
func TestAdaptive_SlowStartToProbing(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 20 * time.Millisecond
	maxQPS := 500.0

	// 基线
	for i := 0; i < baselineWindowCount; i++ {
		feedAndDetect(p, baseLatency, 50)
	}

	// 运行直到阶段不再是 slow_start（最多 30 轮）
	for i := 0; i < 30; i++ {
		lat := simulateLatency(baseLatency, maxQPS, p.EffectiveQPS())
		feedAndDetect(p, lat, 50)
		if p.Phase() != phaseSlowStart {
			break
		}
	}

	if p.Phase() == phaseSlowStart {
		t.Fatalf("在 maxQPS=%.0f 时应退出慢启动, current=%.0f", maxQPS, p.CurrentQPS())
	}
}

// TestAdaptive_ConvergeFastTask 模拟快速任务（10ms），验证收敛到 maxQPS 附近。
func TestAdaptive_ConvergeFastTask(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 10 * time.Millisecond
	maxQPS := 2000.0

	runAdaptive(p, baseLatency, maxQPS, 150)

	finalQPS := p.EffectiveQPS()
	// 应收敛到 maxQPS 的 60%~160% 范围内（算法可能在退避前稍有过冲）
	if finalQPS < maxQPS*0.6 || finalQPS > maxQPS*1.6 {
		t.Errorf("快速任务收敛异常: expected ~%.0f, got %.0f (phase=%s)",
			maxQPS, finalQPS, p.Phase())
	}
	t.Logf("快速任务(10ms, max=2000): converged to %.0f QPS, phase=%s", finalQPS, p.Phase())
}

// TestAdaptive_ConvergeSlowTask 模拟慢速任务（2s），验证收敛。
func TestAdaptive_ConvergeSlowTask(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 2 * time.Second
	maxQPS := 50.0

	runAdaptive(p, baseLatency, maxQPS, 150)

	finalQPS := p.EffectiveQPS()
	// 对于慢速任务，容忍更宽的范围（EWMA 平滑在 warn 阈值附近可能有轻微过冲）
	if finalQPS < maxQPS*0.4 || finalQPS > maxQPS*1.6 {
		t.Errorf("慢速任务收敛异常: expected ~%.0f, got %.0f (phase=%s)",
			maxQPS, finalQPS, p.Phase())
	}
	t.Logf("慢速任务(2s, max=50): converged to %.0f QPS, phase=%s", finalQPS, p.Phase())
}

// TestAdaptive_BackoffAndRecovery 模拟突然的延迟恶化，验证退避与恢复。
func TestAdaptive_BackoffAndRecovery(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 10 * time.Millisecond
	maxQPS := 1000.0

	// 收敛到稳态
	runAdaptive(p, baseLatency, maxQPS, 100)
	convergdQPS := p.EffectiveQPS()
	t.Logf("收敛 QPS: %.0f", convergdQPS)

	// 注入严重延迟（模拟服务降级）
	for i := 0; i < 5; i++ {
		feedAndDetect(p, baseLatency*5, 100) // 5x baseline = critical
	}

	backedOffQPS := p.EffectiveQPS()
	if backedOffQPS >= convergdQPS {
		t.Errorf("退避后 QPS 应下降: before=%.0f after=%.0f", convergdQPS, backedOffQPS)
	}
	t.Logf("退避后 QPS: %.0f", backedOffQPS)

	// 恢复正常延迟，验证探测恢复
	runAdaptive(p, baseLatency, maxQPS, 80)
	recoveredQPS := p.EffectiveQPS()
	if recoveredQPS < convergdQPS*0.5 {
		t.Errorf("恢复后 QPS 应接近原始值: converged=%.0f recovered=%.0f", convergdQPS, recoveredQPS)
	}
	t.Logf("恢复后 QPS: %.0f (phase=%s)", recoveredQPS, p.Phase())
}

// TestAdaptive_CriticalLatencyTriggersBackoff 验证严重延迟必触发退避。
func TestAdaptive_CriticalLatencyTriggersBackoff(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 20 * time.Millisecond

	// 基线
	for i := 0; i < baselineWindowCount; i++ {
		feedAndDetect(p, baseLatency, 50)
	}
	// 慢启动几轮
	for i := 0; i < 3; i++ {
		feedAndDetect(p, baseLatency, 50)
	}

	// 注入临界延迟 (3x baseline > latencyRatioCritical=2.5)
	qpsBefore := p.CurrentQPS()
	feedAndDetect(p, baseLatency*3, 50)

	if p.Phase() != phaseBackoff {
		t.Errorf("临界延迟应触发退避, got phase=%s", p.Phase())
	}
	if p.CurrentQPS() >= qpsBefore {
		t.Errorf("退避后 QPS 应下降: before=%.0f after=%.0f", qpsBefore, p.CurrentQPS())
	}
}

// TestAdaptive_FixedQPSDisabled 验证固定 QPS 模式下控制器不干预。
func TestAdaptive_FixedQPSDisabled(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(5000)

	if p.Phase() != phaseDisabled {
		t.Fatalf("固定 QPS 应处于 disabled, got %s", p.Phase())
	}
	if p.EffectiveQPS() != 5000 {
		t.Fatalf("EffectiveQPS 应等于 targetQPS=5000, got %.0f", p.EffectiveQPS())
	}

	// RecordLatency 应为空操作
	p.RecordLatency(100 * time.Millisecond)
	p.detect() // 不应崩溃或改变状态

	if p.Phase() != phaseDisabled {
		t.Errorf("detect 后仍应为 disabled, got %s", p.Phase())
	}
	if p.EffectiveQPS() != 5000 {
		t.Errorf("EffectiveQPS 不应改变, got %.0f", p.EffectiveQPS())
	}
}

// TestAdaptive_NoSkyrocketLatency 验证延迟不会在探测过程中失控。
func TestAdaptive_NoSkyrocketLatency(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 50 * time.Millisecond
	maxQPS := 500.0
	maxObservedRatio := 0.0

	for i := 0; i < 100; i++ {
		cur := p.EffectiveQPS()
		lat := simulateLatency(baseLatency, maxQPS, cur)
		samples := int(math.Max(cur/10, 10))
		if samples > 300 {
			samples = 300
		}
		feedAndDetect(p, lat, samples)

		// 追踪最大延迟比
		if p.baselineSet && p.baselineP50 > 0 {
			ratio := float64(lat) / p.baselineP50
			if ratio > maxObservedRatio {
				maxObservedRatio = ratio
			}
		}
	}

	// 最大延迟不应超过 4x baseline（算法应在 2.5x 时退避）
	if maxObservedRatio > 4.0 {
		t.Errorf("探测过程中延迟峰值过高: max ratio=%.2f (应<=4.0)", maxObservedRatio)
	}
	t.Logf("最大延迟比: %.2f, final QPS: %.0f", maxObservedRatio, p.EffectiveQPS())
}

// TestAdaptive_PendingGrowthSignal 验证 pending 堆积被视为拥塞信号。
func TestAdaptive_PendingGrowthSignal(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 20 * time.Millisecond

	// 基线
	for i := 0; i < baselineWindowCount; i++ {
		feedAndDetect(p, baseLatency, 50)
	}
	// 几轮慢启动
	for i := 0; i < 5; i++ {
		feedAndDetect(p, baseLatency, 50)
	}

	// 模拟 pending 大量堆积（延迟未变但请求积压）
	for i := 0; i < int(p.CurrentQPS()*3); i++ {
		p.AddPending()
	}

	qpsBefore := p.CurrentQPS()
	feedAndDetect(p, baseLatency, 50) // 延迟正常但 pending 堆积

	// pending 堆积应阻止 QPS 继续增长，或触发回退
	if p.CurrentQPS() > qpsBefore*1.1 {
		t.Errorf("pending 堆积时 QPS 不应继续增长: before=%.0f after=%.0f",
			qpsBefore, p.CurrentQPS())
	}

	// 清理 pending
	pending := p.pending.Load()
	for i := int64(0); i < pending; i++ {
		p.DonePending()
	}
}

// TestAdaptive_SteadyStateSmallProbe 验证稳态下的小幅试探行为。
func TestAdaptive_SteadyStateSmallProbe(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 10 * time.Millisecond
	maxQPS := 1000.0

	// 快速收敛到稳态
	runAdaptive(p, baseLatency, maxQPS, 200)

	if p.Phase() != phaseSteady && p.Phase() != phaseProbing {
		t.Logf("未进入稳态/探测 (phase=%s), 跳过稳态试探测试", p.Phase())
		return
	}

	qpsBeforeProbe := p.EffectiveQPS()

	// 继续跑一段时间，QPS 变化应很小
	runAdaptive(p, baseLatency, maxQPS, 30)

	qpsAfterProbe := p.EffectiveQPS()
	changeRatio := math.Abs(qpsAfterProbe-qpsBeforeProbe) / qpsBeforeProbe
	if changeRatio > 0.15 {
		t.Errorf("稳态 QPS 变化过大: before=%.0f after=%.0f change=%.1f%%",
			qpsBeforeProbe, qpsAfterProbe, changeRatio*100)
	}
	t.Logf("稳态 QPS: before=%.0f after=%.0f change=%.1f%%",
		qpsBeforeProbe, qpsAfterProbe, changeRatio*100)
}

// TestAdaptive_RecordLatencyConcurrent 并发写入延迟不应 panic。
func TestAdaptive_RecordLatencyConcurrent(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	const goroutines = 100
	const perGoroutine = 1000

	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < perGoroutine; j++ {
				p.RecordLatency(10 * time.Millisecond)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// drain 应获取全部样本
	samples := p.drainLatencies()
	if len(samples) != goroutines*perGoroutine {
		t.Errorf("并发写入样本数不匹配: expected %d, got %d",
			goroutines*perGoroutine, len(samples))
	}
}

// TestAdaptive_SnapshotsRecorded 验证每轮 detect 都记录快照。
func TestAdaptive_SnapshotsRecorded(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	for i := 0; i < 10; i++ {
		feedAndDetect(p, 20*time.Millisecond, 50)
	}

	snaps := p.Snapshots()
	if len(snaps) != 10 {
		t.Errorf("应有 10 个快照, got %d", len(snaps))
	}

	// 验证快照包含自适应信息
	last := snaps[len(snaps)-1]
	if last.Phase == "" {
		t.Error("快照应包含 Phase 字段")
	}
}

// TestAdaptive_FlushSnapshots 验证 FlushSnapshots 清空快照。
func TestAdaptive_FlushSnapshots(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	for i := 0; i < 5; i++ {
		feedAndDetect(p, 20*time.Millisecond, 50)
	}

	flushed := p.FlushSnapshots()
	if len(flushed) != 5 {
		t.Errorf("FlushSnapshots 应返回 5 个, got %d", len(flushed))
	}
	remaining := p.Snapshots()
	if len(remaining) != 0 {
		t.Errorf("FlushSnapshots 后应为空, got %d", len(remaining))
	}
}

// TestAdaptive_PhaseString 验证阶段字符串表示。
func TestAdaptive_PhaseString(t *testing.T) {
	tests := []struct {
		phase adaptivePhase
		want  string
	}{
		{phaseDisabled, "disabled"},
		{phaseBaseline, "baseline"},
		{phaseSlowStart, "slow_start"},
		{phaseProbing, "probing"},
		{phaseSteady, "steady"},
		{phaseBackoff, "backoff"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("%d.String() = %s, want %s", int(tt.phase), got, tt.want)
		}
	}
}

// TestAdaptive_LevelMapping 验证延迟比到压力等级的映射。
func TestAdaptive_LevelMapping(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)
	p.baselineP50 = float64(20 * time.Millisecond)
	p.baselineSet = true

	tests := []struct {
		p50ns float64
		want  report.PressureLevel
	}{
		{float64(18 * time.Millisecond), report.PressureLevelNormal},   // ratio=0.9
		{float64(22 * time.Millisecond), report.PressureLevelNormal},   // ratio=1.1 < 1.2 → Normal
		{float64(25 * time.Millisecond), report.PressureLevelWarning},  // ratio=1.25 ∈ [1.2,1.5)
		{float64(32 * time.Millisecond), report.PressureLevelHigh},     // ratio=1.6 ∈ [1.5,2.5)
		{float64(55 * time.Millisecond), report.PressureLevelCritical}, // ratio=2.75 ≥ 2.5
	}

	for _, tt := range tests {
		p.updateLevel(tt.p50ns)
		if p.level != tt.want {
			ratio := tt.p50ns / p.baselineP50
			t.Errorf("P50=%.0fns (ratio=%.2f): got level %d, want %d",
				tt.p50ns, ratio, p.level, tt.want)
		}
	}
}

// TestAdaptive_MinQPSFloor 验证 QPS 不会降到极低值。
func TestAdaptive_MinQPSFloor(t *testing.T) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)

	baseLatency := 20 * time.Millisecond

	// 基线
	for i := 0; i < baselineWindowCount; i++ {
		feedAndDetect(p, baseLatency, 50)
	}

	// 连续注入极端延迟触发多次退避
	for i := 0; i < 30; i++ {
		feedAndDetect(p, baseLatency*10, 50)
	}

	if p.CurrentQPS() < 10 {
		t.Errorf("QPS 不应低于 10, got %.2f", p.CurrentQPS())
	}
}

// ============================================================
// 基准测试
// ============================================================

func BenchmarkPressure_AddDonePending(b *testing.B) {
	p := NewMemoryPressureController()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.AddPending()
		p.DonePending()
	}
}

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

func BenchmarkPressure_RecordLatency_Parallel(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.RecordLatency(10 * time.Millisecond)
		}
	})
}

func BenchmarkPressure_Detect(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(0)
	// 预注入基线
	for i := 0; i < baselineWindowCount; i++ {
		for j := 0; j < 100; j++ {
			p.RecordLatency(10 * time.Millisecond)
		}
		p.detect()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			p.RecordLatency(10 * time.Millisecond)
		}
		p.detect()
	}
}

func BenchmarkPressure_EffectiveQPS(b *testing.B) {
	p := NewMemoryPressureController()
	p.SetTargetQPS(100000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.EffectiveQPS()
	}
}
