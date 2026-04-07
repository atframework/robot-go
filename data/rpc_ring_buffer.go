package atsf4g_go_robot_user

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	base "github.com/atframework/robot-go/base"
)

// RPCRingBuffer 是基于环形数组的 RPC 等待队列，替代 sync.Map。
// 以 sequence % capacity 为索引，支持 Store / Load+Delete。
// 当 slot 被占用（sequence 冲突）时返回错误，调用方应确保 capacity 足够大。
type RPCRingBuffer struct {
	slots    []rpcSlot
	capacity uint64
}

type rpcSlot struct {
	mu       sync.Mutex
	sequence uint64 // 0 表示空闲
	task     base.TaskActionImpl
	occupied atomic.Bool
}

// NewRPCRingBuffer 创建指定容量的环形缓冲区。capacity 必须为 2 的幂。
func NewRPCRingBuffer(capacity int) *RPCRingBuffer {
	// 向上取整到 2 的幂
	cap := uint64(1)
	for cap < uint64(capacity) {
		cap <<= 1
	}
	slots := make([]rpcSlot, cap)
	return &RPCRingBuffer{
		slots:    slots,
		capacity: cap,
	}
}

// Store 将 task 存入 sequence 对应的 slot。
// 如果 slot 已被不同 sequence 占用，返回错误。
func (r *RPCRingBuffer) Store(sequence uint64, task base.TaskActionImpl) error {
	idx := sequence & (r.capacity - 1)
	slot := &r.slots[idx]
	slot.mu.Lock()
	if slot.occupied.Load() && slot.sequence != sequence {
		slot.mu.Unlock()
		return fmt.Errorf("rpc ring buffer slot %d occupied by seq %d, cannot store seq %d", idx, slot.sequence, sequence)
	}
	slot.sequence = sequence
	slot.task = task
	slot.occupied.Store(true)
	slot.mu.Unlock()
	return nil
}

// Load 读取 sequence 对应的 task（不删除）。
func (r *RPCRingBuffer) Load(sequence uint64) (base.TaskActionImpl, bool) {
	idx := sequence & (r.capacity - 1)
	slot := &r.slots[idx]
	if !slot.occupied.Load() {
		return nil, false
	}
	slot.mu.Lock()
	if !slot.occupied.Load() || slot.sequence != sequence {
		slot.mu.Unlock()
		return nil, false
	}
	task := slot.task
	slot.mu.Unlock()
	return task, true
}

// LoadAndDelete 读取并删除 sequence 对应的 task。
func (r *RPCRingBuffer) LoadAndDelete(sequence uint64) (base.TaskActionImpl, bool) {
	idx := sequence & (r.capacity - 1)
	slot := &r.slots[idx]
	if !slot.occupied.Load() {
		return nil, false
	}
	slot.mu.Lock()
	if !slot.occupied.Load() || slot.sequence != sequence {
		slot.mu.Unlock()
		return nil, false
	}
	task := slot.task
	slot.task = nil
	slot.sequence = 0
	slot.occupied.Store(false)
	slot.mu.Unlock()
	return task, true
}

// Delete 删除 sequence 对应的 slot。
func (r *RPCRingBuffer) Delete(sequence uint64) {
	idx := sequence & (r.capacity - 1)
	slot := &r.slots[idx]
	slot.mu.Lock()
	if slot.occupied.Load() && slot.sequence == sequence {
		slot.task = nil
		slot.sequence = 0
		slot.occupied.Store(false)
	}
	slot.mu.Unlock()
}

// StoreBlocking 将 task 存入 sequence 对应的 slot。
// 如果 slot 已被不同 sequence 占用，会自旋等待直到 slot 释放。
func (r *RPCRingBuffer) StoreBlocking(sequence uint64, task base.TaskActionImpl) {
	idx := sequence & (r.capacity - 1)
	slot := &r.slots[idx]
	for {
		slot.mu.Lock()
		if !slot.occupied.Load() || slot.sequence == sequence {
			slot.sequence = sequence
			slot.task = task
			slot.occupied.Store(true)
			slot.mu.Unlock()
			return
		}
		slot.mu.Unlock()
		// slot 被其他 sequence 占用，让出 CPU 后重试
		runtime.Gosched()
	}
}
