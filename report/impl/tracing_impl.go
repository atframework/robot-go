package impl

import (
	"sync"
	"time"

	"github.com/atframework/robot-go/report"
)

// tracingEntry 是 TracingEntry 接口的内存实现
type tracingEntry struct {
	name      string
	tags      []string
	extra     map[string]string
	startTime time.Time
	tracer    *MemoryTracer
	started   bool
}

func (e *tracingEntry) WithTag(tag string) report.TracingEntry {
	e.tags = append(e.tags, tag)
	return e
}

func (e *tracingEntry) WithExtra(key, value string) report.TracingEntry {
	if e.extra == nil {
		e.extra = make(map[string]string)
	}
	e.extra[key] = value
	return e
}

func (e *tracingEntry) Start() report.TracingEntry {
	e.startTime = time.Now()
	e.started = true
	return e
}

func (e *tracingEntry) End(code report.TracingCode) {
	endTime := time.Now()
	if !e.started {
		e.startTime = endTime
	}
	record := &report.TracingRecord{
		Name:       e.name,
		Tags:       e.tags,
		StartTime:  e.startTime,
		EndTime:    endTime,
		DurationMs: endTime.Sub(e.startTime).Milliseconds(),
		Code:       code,
		Extra:      e.extra,
	}
	e.tracer.addRecord(record)
}

func (e *tracingEntry) EndWithError(err error) {
	if err == nil {
		e.End(report.TracingSuccess)
	} else {
		e.WithExtra("error", err.Error())
		e.End(-1)
	}
}

// MemoryTracer 是 Tracer 接口的内存实现，所有数据保存在内存缓冲区中
type MemoryTracer struct {
	mu      sync.Mutex
	records []*report.TracingRecord
}

func NewMemoryTracer() *MemoryTracer {
	return &MemoryTracer{}
}

func (t *MemoryTracer) NewEntry(name string, tags ...string) report.TracingEntry {
	entry := &tracingEntry{
		name:   name,
		tags:   make([]string, len(tags)),
		tracer: t,
	}
	copy(entry.tags, tags)
	return entry
}

func (t *MemoryTracer) addRecord(r *report.TracingRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, r)
}

func (t *MemoryTracer) Flush() []*report.TracingRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	flushed := t.records
	t.records = nil
	return flushed
}

func (t *MemoryTracer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = nil
}

// 编译期验证接口实现
var _ report.Tracer = (*MemoryTracer)(nil)
var _ report.TracingEntry = (*tracingEntry)(nil)
