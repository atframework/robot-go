package impl

import (
	"runtime"
	"runtime/metrics"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atframework/robot-go/report"
)

// ---------- 自适应阶段 ----------

type adaptivePhase int

const (
	phaseDisabled  adaptivePhase = iota // 固定 QPS 模式，控制器不生效
	phaseBaseline                       // 收集基线延迟
	phaseSlowStart                      // 指数增长探测
	phaseProbing                        // 线性增长（加性增）
	phaseSteady                         // 稳态，小幅试探
	phaseBackoff                        // 拥塞退避
)

func (p adaptivePhase) String() string {
	switch p {
	case phaseDisabled:
		return "disabled"
	case phaseBaseline:
		return "baseline"
	case phaseSlowStart:
		return "slow_start"
	case phaseProbing:
		return "probing"
	case phaseSteady:
		return "steady"
	case phaseBackoff:
		return "backoff"
	default:
		return "unknown"
	}
}

// ---------- 算法常量 ----------

const (
	defaultInitialQPS    = 20.0 // 自适应起始 QPS
	latencyRatioOK       = 1.2  // 低于此：健康，可继续增长
	latencyRatioWarn     = 1.5  // 高于此：开始拥塞，停止增长
	latencyRatioCritical = 2.5  // 高于此：严重拥塞，触发退避
	slowStartMultiplier  = 1.3  // 慢启动每轮增长倍数（不用 2x，更保守）
	probeIncreaseRatio   = 0.02 // 线性探测阶段每轮增长比例
	minProbeStep         = 10.0 // 线性探测最小步长
	steadyProbeRatio     = 0.01 // 稳态试探每轮增长比例
	minSteadyStep        = 5.0  // 稳态试探最小步长
	backoffRatio         = 0.85 // 退避倍数（比 TCP 的 0.5 更温和）
	baselineWindowCount  = 5    // 基线收集轮数（更多轮减少基线噪声）
	stableThreshold      = 5    // 稳定轮数门槛（进入稳态）
	backoffCooldown      = 3    // 退避后冷却轮数
	steadyProbeInterval  = 5    // 稳态周期性探测间隔（轮）
	ewmaAlpha            = 0.3  // EWMA 平滑系数（新样本权重）
	slowStartWarnConfirm = 2    // 慢启动需连续 N 轮警告才确认压力
)

// ---------- MemoryPressureController ----------

// MemoryPressureController 实现 report.PressureController。
//
// 两种模式：
//   - 固定 QPS（targetQPS > 0）：所有方法为空操作，不影响执行。
//   - 自适应（targetQPS <= 0）：基于延迟反馈的 AIMD 拥塞控制，从低 QPS 探测到最优速率。
type MemoryPressureController struct {
	// 用户配置
	targetQPS float64 // >0: 固定模式, <=0: 自适应模式

	// 自适应状态
	phase       adaptivePhase
	currentQPS  float64 // 当前推荐 QPS
	safeQPS     float64 // 上次确认安全的 QPS
	baselineP50 float64 // 基线 P50 延迟（纳秒）
	baselineSet bool

	// 基线收集
	baselineCollecting int       // 已收集轮数
	baselineSum        float64   // 累计 P50（纳秒）
	baselineP50s       []float64 // 各轮 P50，用于取中位数

	// 延迟采集
	latencyMu  sync.Mutex
	latencies  []time.Duration // 每轮 detect 时 drain
	latencyBuf []time.Duration // drain 用复用缓冲

	// Pending 追踪
	pending     atomic.Int64
	lastPending int64

	// 状态机计数器
	stableCount   int  // 连续稳定轮数
	backoffWait   int  // 退避剩余冷却轮数
	steadyCounter int  // 稳态探测计数
	probingUp     bool // 稳态是否正在向上探测

	// EWMA 平滑（消除低 QPS 时延迟抖动对决策的干扰）
	smoothedRatio float64 // 延迟比的指数加权移动平均
	smoothedSet   bool    // smoothedRatio 是否已初始化
	warnStreak    int     // 慢启动阶段连续警告轮数

	// 快照 & 等级
	snapshots []report.PressureSnapshot
	level     report.PressureLevel

	// 生命周期
	stopCh  chan struct{}
	running bool
}

func NewMemoryPressureController() *MemoryPressureController {
	return &MemoryPressureController{
		currentQPS: defaultInitialQPS,
	}
}

// ---------- 接口实现 ----------

func (p *MemoryPressureController) SetTargetQPS(qps float64) {
	p.targetQPS = qps
	if qps > 0 {
		p.phase = phaseDisabled
		p.currentQPS = qps
	} else {
		p.phase = phaseBaseline
		p.currentQPS = defaultInitialQPS
	}
}

func (p *MemoryPressureController) EffectiveQPS() float64 {
	if p.targetQPS > 0 {
		return p.targetQPS
	}
	return p.currentQPS
}

func (p *MemoryPressureController) AddPending()                        { p.pending.Add(1) }
func (p *MemoryPressureController) DonePending()                       { p.pending.Add(-1) }
func (p *MemoryPressureController) CurrentLevel() report.PressureLevel { return p.level }

func (p *MemoryPressureController) RecordLatency(d time.Duration) {
	if p.targetQPS > 0 {
		return // 固定模式不收集
	}
	p.latencyMu.Lock()
	p.latencies = append(p.latencies, d)
	p.latencyMu.Unlock()
}

func (p *MemoryPressureController) Snapshots() []report.PressureSnapshot {
	cp := make([]report.PressureSnapshot, len(p.snapshots))
	copy(cp, p.snapshots)
	return cp
}

func (p *MemoryPressureController) FlushSnapshots() []report.PressureSnapshot {
	snaps := p.snapshots
	p.snapshots = nil
	return snaps
}

func (p *MemoryPressureController) Start(interval time.Duration) {
	if p.targetQPS > 0 || p.running {
		return // 固定模式不启动检测循环
	}
	p.stopCh = make(chan struct{})
	p.running = true
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.detect()
			case <-p.stopCh:
				return
			}
		}
	}()
}

func (p *MemoryPressureController) Stop() {
	if p.running {
		close(p.stopCh)
		p.running = false
	}
}

// ---------- 核心检测逻辑 ----------

// detect 每轮执行：排空延迟样本 → 计算 P50 → 根据阶段决策 QPS 调整。
func (p *MemoryPressureController) detect() {
	if p.phase == phaseDisabled {
		return
	}

	samples := p.drainLatencies()
	currentPending := p.pending.Load()

	goroutines := runtime.NumGoroutine()
	heapMB := readHeapMB()

	if len(samples) < 3 {
		// 样本不足，不做决策
		p.recordSnapshot(goroutines, heapMB, currentPending, 0)
		return
	}

	p50 := computeP50(samples)
	p50ns := float64(p50.Nanoseconds())

	// 更新 EWMA 平滑比值（仅在基线已建立后生效）
	if p.baselineSet && p.baselineP50 > 0 {
		p.updateSmoothedRatio(p50ns / p.baselineP50)
	}

	// Pending 增长检测：如果 pending 持续堆积，视为拥塞信号
	pendingGrowing := currentPending > p.lastPending+int64(p.currentQPS*0.5) &&
		currentPending > int64(p.currentQPS*1.5)
	p.lastPending = currentPending

	switch p.phase {
	case phaseBaseline:
		p.doBaseline(p50ns)
	case phaseSlowStart:
		p.doSlowStart(p50ns, currentPending, pendingGrowing)
	case phaseProbing:
		p.doProbing(p50ns, pendingGrowing)
	case phaseSteady:
		p.doSteady(p50ns, pendingGrowing)
	case phaseBackoff:
		p.doBackoff()
	}

	p.updateLevel(p50ns)
	p.recordSnapshot(goroutines, heapMB, currentPending, p50ns)
}

// doBaseline 收集基线延迟（前 N 轮低速运行时的 P50）。
func (p *MemoryPressureController) doBaseline(p50ns float64) {
	p.baselineCollecting++
	p.baselineP50s = append(p.baselineP50s, p50ns)
	p.baselineSum += p50ns
	if p.baselineCollecting >= baselineWindowCount {
		// 取中位数作为基线，抗抖动
		sorted := make([]float64, len(p.baselineP50s))
		copy(sorted, p.baselineP50s)
		sort.Float64s(sorted)
		p.baselineP50 = sorted[len(sorted)/2]
		p.baselineSet = true
		p.smoothedRatio = 1.0 // 基线比值定义为 1.0
		p.smoothedSet = true
		p.phase = phaseSlowStart
		p.safeQPS = p.currentQPS
	}
}

// doSlowStart 指数增长阶段：每轮 ×1.8 直到延迟或 pending 出现信号。
// 使用原始延迟比 + warnStreak 连续确认：慢启动增长快（1.8x/轮），EWMA 来不及跟踪，
// 直接用原始值判断更准确；warnStreak 过滤单轮抖动。
func (p *MemoryPressureController) doSlowStart(p50ns float64, currentPending int64, pendingGrowing bool) {
	rawRatio := p50ns / p.baselineP50

	// 临界拥塞：立即退避
	if rawRatio >= latencyRatioCritical {
		p.warnStreak = 0
		p.enterBackoff()
		return
	}

	// 增长 / 警告用原始比值 + warnStreak 过滤抖动
	if (rawRatio < latencyRatioWarn) && !pendingGrowing && currentPending < int64(p.currentQPS*2) {
		// 健康：继续指数增长
		p.warnStreak = 0
		p.safeQPS = p.currentQPS
		p.currentQPS *= slowStartMultiplier
	} else {
		// 检测到压力信号，需要连续确认以排除抖动
		p.warnStreak++
		if p.warnStreak >= slowStartWarnConfirm {
			// 连续多轮确认，回退到安全值，进入线性探测
			p.currentQPS = p.safeQPS
			p.phase = phaseProbing
			p.stableCount = 0
			p.warnStreak = 0
		}
		// 否则：单轮波动，保持当前 QPS 不增长，等待下一轮确认
	}
}

// doProbing 线性探测阶段：每轮 +4% 寻找上限。使用 EWMA 平滑值判断趋势。
func (p *MemoryPressureController) doProbing(p50ns float64, pendingGrowing bool) {
	// 临界拥塞：原始值判断（紧急信号不平滑）
	if p.baselineP50 > 0 && p50ns/p.baselineP50 >= latencyRatioCritical {
		p.enterBackoff()
		return
	}

	ratio := p.smoothedRatio
	if pendingGrowing && ratio < latencyRatioWarn {
		ratio = latencyRatioWarn // pending 堆积视为轻度拥塞
	}

	if ratio < latencyRatioOK {
		// 健康区：继续增长
		step := p.currentQPS * probeIncreaseRatio
		if step < minProbeStep {
			step = minProbeStep
		}
		p.safeQPS = p.currentQPS
		p.currentQPS += step
		p.stableCount = 0
	} else if ratio < latencyRatioWarn {
		// 边缘区：保持不变，累计稳定计数
		p.safeQPS = p.currentQPS
		p.stableCount++
		if p.stableCount >= stableThreshold {
			p.phase = phaseSteady
			p.steadyCounter = 0
			p.probingUp = false
		}
	} else {
		// 轻度拥塞（EWMA 值 ≥ warn）：小幅回退
		p.currentQPS *= 0.95
		if p.currentQPS < p.safeQPS*0.8 {
			p.currentQPS = p.safeQPS * 0.8
		}
		p.stableCount = 0
	}
}

// doSteady 稳态阶段：周期性小幅试探，发现环境变化时调整。使用 EWMA 平滑值。
func (p *MemoryPressureController) doSteady(p50ns float64, pendingGrowing bool) {
	// 临界拥塞：原始值判断
	if p.baselineP50 > 0 && p50ns/p.baselineP50 >= latencyRatioCritical {
		p.enterBackoff()
		return
	}

	ratio := p.smoothedRatio
	if pendingGrowing && ratio < latencyRatioWarn {
		ratio = latencyRatioWarn
	}

	p.steadyCounter++

	if p.probingUp {
		if ratio >= latencyRatioWarn {
			// 试探失败，回退
			p.currentQPS = p.safeQPS
			p.probingUp = false
		} else if ratio < latencyRatioOK {
			// 试探成功，提升安全值
			p.safeQPS = p.currentQPS
			p.probingUp = false
		}
		// else: 还在观察中
		return
	}

	if ratio >= latencyRatioWarn {
		// 环境恶化，小幅回退
		p.currentQPS *= 0.95
		if p.currentQPS < p.safeQPS*0.8 {
			p.currentQPS = p.safeQPS * 0.8
		}
		return
	}

	if p.steadyCounter >= steadyProbeInterval {
		// 周期性向上试探
		step := p.currentQPS * steadyProbeRatio
		if step < minSteadyStep {
			step = minSteadyStep
		}
		p.currentQPS += step
		p.steadyCounter = 0
		p.probingUp = true
	}
}

// doBackoff 退避冷却阶段：等待数轮后重新进入线性探测。
func (p *MemoryPressureController) doBackoff() {
	p.backoffWait--
	if p.backoffWait <= 0 {
		p.phase = phaseProbing
		p.stableCount = 0
	}
}

func (p *MemoryPressureController) enterBackoff() {
	p.phase = phaseBackoff
	p.backoffWait = backoffCooldown
	p.currentQPS *= backoffRatio
	// 不低于安全值的一半
	if p.safeQPS > 0 && p.currentQPS < p.safeQPS*0.5 {
		p.currentQPS = p.safeQPS * 0.5
	}
	if p.currentQPS < 10 {
		p.currentQPS = 10
	}
}

// ---------- 辅助方法 ----------

func (p *MemoryPressureController) drainLatencies() []time.Duration {
	p.latencyMu.Lock()
	if len(p.latencies) == 0 {
		p.latencyMu.Unlock()
		return nil
	}
	// 复用 buf 减少分配
	p.latencyBuf = append(p.latencyBuf[:0], p.latencies...)
	p.latencies = p.latencies[:0]
	p.latencyMu.Unlock()
	return p.latencyBuf
}

// updateSmoothedRatio 使用 EWMA（指数加权移动平均）更新延迟比。
// 在低 QPS 时样本量少、P50 抖动大，EWMA 可以有效平滑噪声，
// 避免因单轮毛刺而过早退出慢启动。
func (p *MemoryPressureController) updateSmoothedRatio(rawRatio float64) {
	if !p.smoothedSet {
		p.smoothedRatio = rawRatio
		p.smoothedSet = true
	} else {
		p.smoothedRatio = ewmaAlpha*rawRatio + (1-ewmaAlpha)*p.smoothedRatio
	}
}

func (p *MemoryPressureController) updateLevel(p50ns float64) {
	if !p.baselineSet || p.baselineP50 <= 0 {
		p.level = report.PressureLevelNormal
		return
	}
	ratio := p50ns / p.baselineP50
	switch {
	case ratio >= latencyRatioCritical:
		p.level = report.PressureLevelCritical
	case ratio >= latencyRatioWarn:
		p.level = report.PressureLevelHigh
	case ratio >= latencyRatioOK:
		p.level = report.PressureLevelWarning
	default:
		p.level = report.PressureLevelNormal
	}
}

func (p *MemoryPressureController) recordSnapshot(goroutines int, heapMB float64, pending int64, p50ns float64) {
	snap := report.PressureSnapshot{
		Timestamp:      time.Now(),
		Level:          p.level,
		GoroutineCount: goroutines,
		HeapAllocMB:    heapMB,
		PendingReqs:    pending,
		TargetQPS:      p.currentQPS,
		ActualQPS:      p.currentQPS,
		Phase:          p.phase.String(),
	}
	if p.baselineSet && p.baselineP50 > 0 {
		snap.BaselineP50Ms = p.baselineP50 / float64(time.Millisecond)
	}
	if p50ns > 0 {
		snap.LatencyP50Ms = p50ns / float64(time.Millisecond)
	}
	p.snapshots = append(p.snapshots, snap)
}

func computeP50(samples []time.Duration) time.Duration {
	n := len(samples)
	if n == 0 {
		return 0
	}
	sorted := make([]time.Duration, n)
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[n/2]
}

func readHeapMB() float64 {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
	}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindUint64 {
		return float64(samples[0].Value.Uint64()) / (1024 * 1024)
	}
	return 0
}

// Phase 返回当前自适应阶段（供测试使用）。
func (p *MemoryPressureController) Phase() adaptivePhase { return p.phase }

// CurrentQPS 返回当前内部 QPS（供测试使用）。
func (p *MemoryPressureController) CurrentQPS() float64 { return p.currentQPS }

// SafeQPS 返回上次确认安全的 QPS（供测试使用）。
func (p *MemoryPressureController) SafeQPS() float64 { return p.safeQPS }

// BaselineP50Ns 返回基线 P50（纳秒，供测试使用）。
func (p *MemoryPressureController) BaselineP50Ns() float64 { return p.baselineP50 }

var _ report.PressureController = (*MemoryPressureController)(nil)
