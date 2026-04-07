package impl

import (
	"math"
	"runtime"
	"runtime/metrics"
	"sync/atomic"
	"time"

	"github.com/atframework/robot-go/report"
)

// PressureThresholds 压力判定阈值（可由外部配置覆盖）
type PressureThresholds struct {
	GoroutineWarning  int     // 默认 10000
	GoroutineHigh     int     // 默认 50000
	GoroutineCritical int     // 默认 100000
	HeapMBWarning     float64 // 默认 512
	HeapMBHigh        float64 // 默认 1024
	HeapMBCritical    float64 // 默认 2048
	PendingMultWarn   float64 // pending > targetConcurrency * mult → warning, 默认 2
	PendingMultHigh   float64 // 默认 5
	PendingMultCrit   float64 // 默认 10
}

func DefaultPressureThresholds() PressureThresholds {
	return PressureThresholds{
		GoroutineWarning:  10000,
		GoroutineHigh:     50000,
		GoroutineCritical: 100000,
		HeapMBWarning:     512,
		HeapMBHigh:        1024,
		HeapMBCritical:    2048,
		PendingMultWarn:   2,
		PendingMultHigh:   5,
		PendingMultCrit:   10,
	}
}

// MemoryPressureController 是 PressureController 的内存实现
type MemoryPressureController struct {
	targetQPS     float64
	throttleRatio float64 // 1.0 = 不限速
	level         report.PressureLevel
	pending       atomic.Int64
	snapshots     []report.PressureSnapshot
	thresholds    PressureThresholds
	stopCh        chan struct{}
	running       bool
}

func NewMemoryPressureController(thresholds ...PressureThresholds) *MemoryPressureController {
	t := DefaultPressureThresholds()
	if len(thresholds) > 0 {
		t = thresholds[0]
	}
	return &MemoryPressureController{
		throttleRatio: 1.0,
		thresholds:    t,
	}
}

func (p *MemoryPressureController) SetTargetQPS(qps float64) {
	p.targetQPS = qps
}

func (p *MemoryPressureController) EffectiveQPS() float64 {
	return p.targetQPS * p.throttleRatio
}

func (p *MemoryPressureController) AddPending() {
	p.pending.Add(1)
}

func (p *MemoryPressureController) DonePending() {
	p.pending.Add(-1)
}

func (p *MemoryPressureController) Start(interval time.Duration) {
	if p.running {
		return
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

func (p *MemoryPressureController) CurrentLevel() report.PressureLevel {
	return p.level
}

func (p *MemoryPressureController) Snapshots() []report.PressureSnapshot {
	cp := make([]report.PressureSnapshot, len(p.snapshots))
	copy(cp, p.snapshots)
	return cp
}

// FlushSnapshots 返回自上次 Flush 以来新增的快照并清空内部缓冲，避免重复写入。
func (p *MemoryPressureController) FlushSnapshots() []report.PressureSnapshot {
	snaps := p.snapshots
	p.snapshots = nil
	return snaps
}

func (p *MemoryPressureController) detect() {
	// 使用 runtime/metrics 代替 runtime.ReadMemStats，避免 STW 停顿
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
	}
	metrics.Read(samples)
	var heapBytes uint64
	if samples[0].Value.Kind() == metrics.KindUint64 {
		heapBytes = samples[0].Value.Uint64()
	}

	goroutines := runtime.NumGoroutine()
	heapMB := float64(heapBytes) / (1024 * 1024)
	pendingReqs := p.pending.Load()

	newLevel := p.calcLevel(goroutines, heapMB, pendingReqs)

	oldLevel := p.level

	// 等级上升：立即生效
	if newLevel > oldLevel {
		p.level = newLevel
		p.throttleRatio = throttleForLevel(newLevel)
	} else if newLevel < oldLevel {
		// 等级下降：渐进恢复，每次最多提升 5%
		targetRatio := throttleForLevel(newLevel)
		p.throttleRatio = math.Min(targetRatio, p.throttleRatio+0.05)
		// 只在 ratio 完全恢复到目标时才降级
		if p.throttleRatio >= targetRatio {
			p.level = newLevel
		}
	}

	snap := report.PressureSnapshot{
		Timestamp:      time.Now(),
		Level:          p.level,
		GoroutineCount: goroutines,
		HeapAllocMB:    heapMB,
		PendingReqs:    pendingReqs,
		TargetQPS:      p.targetQPS,
		ActualQPS:      p.targetQPS * p.throttleRatio,
		ThrottleRatio:  p.throttleRatio,
	}
	p.snapshots = append(p.snapshots, snap)
}

func (p *MemoryPressureController) calcLevel(goroutines int, heapMB float64, pending int64) report.PressureLevel {
	t := p.thresholds
	level := report.PressureLevelNormal

	// 取各维度最高等级
	if goroutines > t.GoroutineCritical {
		level = max(level, report.PressureLevelCritical)
	} else if goroutines > t.GoroutineHigh {
		level = max(level, report.PressureLevelHigh)
	} else if goroutines > t.GoroutineWarning {
		level = max(level, report.PressureLevelWarning)
	}

	if heapMB > t.HeapMBCritical {
		level = max(level, report.PressureLevelCritical)
	} else if heapMB > t.HeapMBHigh {
		level = max(level, report.PressureLevelHigh)
	} else if heapMB > t.HeapMBWarning {
		level = max(level, report.PressureLevelWarning)
	}

	targetConcurrency := p.targetQPS
	if targetConcurrency > 0 {
		pendingF := float64(pending)
		if pendingF > targetConcurrency*t.PendingMultCrit {
			level = max(level, report.PressureLevelCritical)
		} else if pendingF > targetConcurrency*t.PendingMultHigh {
			level = max(level, report.PressureLevelHigh)
		} else if pendingF > targetConcurrency*t.PendingMultWarn {
			level = max(level, report.PressureLevelWarning)
		}
	}

	return level
}

func throttleForLevel(l report.PressureLevel) float64 {
	switch l {
	case report.PressureLevelWarning:
		return 0.9
	case report.PressureLevelHigh:
		return 0.6
	case report.PressureLevelCritical:
		return 0.2
	default:
		return 1.0
	}
}

func max(a, b report.PressureLevel) report.PressureLevel {
	if a > b {
		return a
	}
	return b
}

var _ report.PressureController = (*MemoryPressureController)(nil)
