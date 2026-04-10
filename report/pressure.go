package report

import "time"

// PressureLevel 压力等级
type PressureLevel int

const (
	PressureLevelNormal   PressureLevel = 0 // 正常
	PressureLevelWarning  PressureLevel = 1 // 警告
	PressureLevelHigh     PressureLevel = 2 // 高压
	PressureLevelCritical PressureLevel = 3 // 临界
)

// PressureSnapshot 某一时刻的压力快照
type PressureSnapshot struct {
	Timestamp      time.Time     `json:"timestamp"`
	Level          PressureLevel `json:"level"`
	GoroutineCount int           `json:"goroutine_count"`
	HeapAllocMB    float64       `json:"heap_alloc_mb"`
	PendingReqs    int64         `json:"pending_requests"`
	TargetQPS      float64       `json:"target_qps"`
	ActualQPS      float64       `json:"actual_qps"`
	ThrottleRatio  float64       `json:"throttle_ratio"`
	// 自适应模式附加字段
	LatencyP50Ms  float64 `json:"latency_p50_ms,omitempty"`
	BaselineP50Ms float64 `json:"baseline_p50_ms,omitempty"`
	Phase         string  `json:"phase,omitempty"` // baseline / slow_start / probing / steady / backoff
}

// PressureController 自压力检测与 QPS 自适应控制器。
//
// 两种工作模式：
//   - 固定 QPS 模式（SetTargetQPS > 0）：控制器不生效，直接按给定 QPS 执行。
//   - 自适应模式（SetTargetQPS <= 0）：从低 QPS 向高 QPS 探测，基于延迟反馈自动找到最优速率。
type PressureController interface {
	// SetTargetQPS 设置目标 QPS；>0 表示固定模式（控制器禁用），<=0 表示自适应模式。
	SetTargetQPS(qps float64)
	// EffectiveQPS 返回当前推荐的 QPS（自适应模式由控制器决定，固定模式返回 targetQPS）
	EffectiveQPS() float64
	// AddPending 请求开始，增加待处理计数
	AddPending()
	// DonePending 请求完成，减少待处理计数
	DonePending()
	// RecordLatency 记录一次请求的完成延迟（自适应模式用于拥塞检测）
	RecordLatency(d time.Duration)
	// Start 启动后台检测
	Start(interval time.Duration)
	// Stop 停止检测
	Stop()
	// CurrentLevel 返回当前压力等级
	CurrentLevel() PressureLevel
	// Snapshots 返回全部快照
	Snapshots() []PressureSnapshot
}
