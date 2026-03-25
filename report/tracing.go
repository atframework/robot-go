package report

import "time"

// TracingCode 打点结果码，0 表示成功，非 0 表示失败
type TracingCode = int32

const TracingSuccess TracingCode = 0

// TracingRecord 单次打点的完整数据
type TracingRecord struct {
	Name       string            `json:"name"`
	Tags       []string          `json:"tags,omitempty"`
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	DurationMs int64             `json:"duration_ms"`
	Code       TracingCode       `json:"code"`
	Extra      map[string]string `json:"extra,omitempty"`
}

// TracingEntry 代表一次正在进行的打点操作
type TracingEntry interface {
	// WithTag 追加 tag，支持链式调用
	WithTag(tag string) TracingEntry
	// WithExtra 设置额外 KV 信息，支持链式调用
	WithExtra(key, value string) TracingEntry
	// Start 开始计时，支持链式调用
	Start() TracingEntry
	// End 结束计时并记录结果码
	End(code TracingCode)
	// EndWithError 结束计时，非 nil error 视为失败（code = -1）
	EndWithError(err error)
}

// Tracer 管理打点的生命周期和数据收集
type Tracer interface {
	// NewEntry 创建一个新的打点实例（尚未开始计时）
	NewEntry(name string, tags ...string) TracingEntry
	// Flush 返回快照并清空内部缓冲
	Flush() []*TracingRecord
	// Reset 清空所有数据
	Reset()
}
