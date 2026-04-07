package impl

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atframework/robot-go/report"
)

// tracingEntry 是 TracingEntry 接口的内存实现
type tracingEntry struct {
	name      string
	startTime time.Time
	tracer    *MemoryTracer
	started   bool
}

var tracingEntryPool = sync.Pool{
	New: func() any { return &tracingEntry{} },
}

func (e *tracingEntry) Start() report.TracingEntry {
	e.startTime = time.Now()
	e.started = true
	return e
}

func (e *tracingEntry) End(code report.TracingCode, errMsg string) {
	endTime := time.Now()
	if !e.started {
		e.startTime = endTime
	}
	// 热路径：不分配 TracingRecord，直接传递原始字段给 addRecord 原地聚合
	e.tracer.addRecord(e.name, e.startTime.Unix(), endTime.Unix(),
		endTime.Sub(e.startTime).Milliseconds(), int(code), errMsg)
	// 归还到池
	e.name = ""
	e.tracer = nil
	e.started = false
	tracingEntryPool.Put(e)
}

func (e *tracingEntry) EndWithError(err error) {
	if err == nil {
		e.End(report.TracingSuccess, "")
	} else {
		e.End(-1, err.Error())
	}
}

// --- 双缓冲内部结构 ---

type tracingBucketKey struct {
	name      string
	timestamp int64
	startData bool
}

// tracingBucket 是单个 (name, second, isStart) 的可变聚合状态。
// 通过 sync.Pool 复用，hot path 中不产生堆分配。
type tracingBucket struct {
	count       int
	totalMs     int64
	minMs       int64 // -1 = unset
	maxMs       int64
	meanMs      float64 // Welford 在线均值
	m2Ms        float64 // Welford M2（偏差平方和，用于方差）
	codeCounts  map[int]int
	errorCounts map[string]int
}

var tracingBucketPool = sync.Pool{
	New: func() any { return &tracingBucket{minMs: -1} },
}

func (b *tracingBucket) reset() {
	b.count = 0
	b.totalMs = 0
	b.minMs = -1
	b.maxMs = 0
	b.meanMs = 0
	b.m2Ms = 0
	for k := range b.codeCounts {
		delete(b.codeCounts, k)
	}
	for k := range b.errorCounts {
		delete(b.errorCounts, k)
	}
}

// updateEnd 更新 end 记录的统计（Welford 在线算法，无浮点数堆分配）
func (b *tracingBucket) updateEnd(durMs int64, code int, errMsg string) {
	b.count++
	b.totalMs += durMs
	if b.minMs < 0 || durMs < b.minMs {
		b.minMs = durMs
	}
	if durMs > b.maxMs {
		b.maxMs = durMs
	}
	delta := float64(durMs) - b.meanMs
	b.meanMs += delta / float64(b.count)
	b.m2Ms += delta * (float64(durMs) - b.meanMs)
	if b.codeCounts == nil {
		b.codeCounts = make(map[int]int, 4)
	}
	b.codeCounts[code]++
	if errMsg != "" && code != int(report.TracingSuccess) {
		if b.errorCounts == nil {
			b.errorCounts = make(map[string]int, 4)
		}
		b.errorCounts[errMsg]++
	}
}

type tracingBank struct {
	mu      sync.Mutex
	buckets map[tracingBucketKey]*tracingBucket
	order   []tracingBucketKey // 插入顺序，保证 snapshot 输出稳定
}

func newTracingBank() tracingBank {
	return tracingBank{
		buckets: make(map[tracingBucketKey]*tracingBucket),
	}
}

func (b *tracingBank) getOrCreate(key tracingBucketKey) *tracingBucket {
	bk := b.buckets[key]
	if bk == nil {
		bk = tracingBucketPool.Get().(*tracingBucket)
		bk.reset()
		b.buckets[key] = bk
		b.order = append(b.order, key)
	}
	return bk
}

func (b *tracingBank) snapshot() []*report.TracingRecord {
	if len(b.buckets) == 0 {
		return nil
	}
	result := make([]*report.TracingRecord, 0, len(b.buckets))
	for _, key := range b.order {
		bk := b.buckets[key]
		if bk == nil || bk.count == 0 {
			continue
		}
		rec := &report.TracingRecord{
			Timestamp: key.timestamp,
			StartData: key.startData,
			Name:      key.name,
			Count:     bk.count,
		}
		if !key.startData {
			rec.TotalDurationMs = bk.totalMs
			if bk.minMs >= 0 {
				rec.MinDurationMs = bk.minMs
			}
			rec.MaxDurationMs = bk.maxMs
			if bk.count > 1 {
				rec.Variance = int64(bk.m2Ms / float64(bk.count))
			}
			if len(bk.codeCounts) > 0 {
				rec.Code = make(map[int]int, len(bk.codeCounts))
				for k, v := range bk.codeCounts {
					rec.Code[k] = v
				}
			}
			if len(bk.errorCounts) > 0 {
				rec.Error = make(map[string]int, len(bk.errorCounts))
				for k, v := range bk.errorCounts {
					rec.Error[k] = v
				}
			}
		}
		result = append(result, rec)
	}
	return result
}

func (b *tracingBank) clear() {
	for _, bk := range b.buckets {
		if bk != nil {
			tracingBucketPool.Put(bk)
		}
	}
	// 保留底层数组，只清空 map 和 order
	for k := range b.buckets {
		delete(b.buckets, k)
	}
	b.order = b.order[:0]
}

// MemoryTracer 分片双缓冲区实现。
// 写入路径按 goroutine 哈希选择 shard，每个 shard 独立双缓冲，大幅降低锁竞争。
// Flush 时切换所有 shard 的活跃 bank，然后合并各 shard 的旧 bank 数据。
type MemoryTracer struct {
	shards    []*tracerShard
	shardMask uint64 // shardCount - 1，用于位与取模
}

type tracerShard struct {
	active int32 // atomic: 0 or 1
	banks  [2]tracingBank
}

// defaultShardCount 返回分片数（向上取 2 的幂，基于 CPU 核数）
func defaultShardCount() int {
	n := runtime.GOMAXPROCS(0)
	if n < 4 {
		n = 4
	}
	// 向上取 2 的幂
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

func NewMemoryTracer() *MemoryTracer {
	return NewMemoryTracerWithShards(defaultShardCount())
}

// NewMemoryTracerWithShards 创建指定分片数的 MemoryTracer。shardCount 必须为 2 的幂。
func NewMemoryTracerWithShards(shardCount int) *MemoryTracer {
	if shardCount <= 0 {
		shardCount = 1
	}
	// 向上取 2 的幂
	p := uint64(1)
	for p < uint64(shardCount) {
		p <<= 1
	}
	shards := make([]*tracerShard, p)
	for i := range shards {
		shards[i] = &tracerShard{}
		shards[i].banks[0] = newTracingBank()
		shards[i].banks[1] = newTracingBank()
	}
	return &MemoryTracer{
		shards:    shards,
		shardMask: p - 1,
	}
}

func (t *MemoryTracer) NewEntry(name string) report.TracingEntry {
	e := tracingEntryPool.Get().(*tracingEntry)
	e.name = name
	e.tracer = t
	e.started = false
	return e
}

// pickShard 使用快速哈希选择 shard，分散写入压力。
// 基于 name 字符串的简单 FNV-like 哈希。
func (t *MemoryTracer) pickShard(name string) *tracerShard {
	h := uint64(14695981039346656037) // FNV offset basis
	for i := 0; i < len(name); i++ {
		h ^= uint64(name[i])
		h *= 1099511628211 // FNV prime
	}
	return t.shards[h&t.shardMask]
}

// addRecord 热路径：按 name 哈希选 shard → atomic 读活跃 bank → 锁该 shard 的 bank → 原地聚合。
func (t *MemoryTracer) addRecord(name string, startSec, endSec, durMs int64, code int, errMsg string) {
	shard := t.pickShard(name)
	bankIdx := atomic.LoadInt32(&shard.active)
	bank := &shard.banks[bankIdx]
	bank.mu.Lock()

	endBk := bank.getOrCreate(tracingBucketKey{name: name, timestamp: endSec, startData: false})
	endBk.updateEnd(durMs, code, errMsg)

	startBk := bank.getOrCreate(tracingBucketKey{name: name, timestamp: startSec, startData: true})
	startBk.count++

	bank.mu.Unlock()
}

// Flush 原子切换所有 shard 的活跃 bank，然后收割并合并各 shard 的旧 bank 数据。
func (t *MemoryTracer) Flush() []*report.TracingRecord {
	// 阶段 1：切换所有 shard
	for _, shard := range t.shards {
		oldIdx := atomic.LoadInt32(&shard.active)
		newIdx := 1 - oldIdx
		newBank := &shard.banks[newIdx]
		newBank.mu.Lock()
		newBank.clear()
		newBank.mu.Unlock()
		atomic.StoreInt32(&shard.active, int32(newIdx))
	}

	// 阶段 2：收割并合并
	var allRecords []*report.TracingRecord
	for _, shard := range t.shards {
		// 旧 bank 的 index = 1 - 当前活跃
		oldIdx := 1 - atomic.LoadInt32(&shard.active)
		oldBank := &shard.banks[oldIdx]
		oldBank.mu.Lock()
		records := oldBank.snapshot()
		oldBank.clear()
		oldBank.mu.Unlock()
		allRecords = append(allRecords, records...)
	}

	// 合并相同 key 的记录（不同 shard 可能有相同 name 但不同 timestamp，
	// 但同一 name 在 pickShard 中映射到同一 shard，所以实际不需要跨 shard 合并）
	return allRecords
}

func (t *MemoryTracer) Reset() {
	for _, shard := range t.shards {
		for i := range shard.banks {
			shard.banks[i].mu.Lock()
			shard.banks[i].clear()
			shard.banks[i].mu.Unlock()
		}
	}
}

// 编译期验证接口实现
var _ report.Tracer = (*MemoryTracer)(nil)
var _ report.TracingEntry = (*tracingEntry)(nil)
