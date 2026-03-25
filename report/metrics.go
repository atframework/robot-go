package report

import "time"

// MetricsValueFunc 指标采集回调，返回当前瞬时值
type MetricsValueFunc func() float64

// MetricsPoint 单次采集数据点
type MetricsPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// MetricsSeries 某一指标的完整时间序列
type MetricsSeries struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Points []MetricsPoint    `json:"points"`
}

// MetricsCollector 指标收集器
type MetricsCollector interface {
	// Register 注册一个指标采集函数
	Register(name string, fn MetricsValueFunc)
	// RegisterWithLabels 注册带标签的指标
	RegisterWithLabels(name string, labels map[string]string, fn MetricsValueFunc)
	// Unregister 取消注册
	Unregister(name string)

	// Collect 手动触发一次采集
	Collect()
	// StartAutoCollect 启动按固定间隔自动采集
	StartAutoCollect(interval time.Duration)
	// StopAutoCollect 停止自动采集
	StopAutoCollect()

	// Flush 返回快照并清空
	Flush() []*MetricsSeries
	// Reset 清空所有数据
	Reset()
}
