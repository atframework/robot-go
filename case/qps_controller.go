package atsf4g_go_robot_case

import (
	"sync"
	"sync/atomic"
	"time"
)

// QPSController 令牌桶限速器。
//
// 设计思路（"生产者-消费者"分离模型）：
//   - 一个独立的 refiller goroutine 以固定间隔补充令牌并通过 Broadcast 唤醒所有等待者；
//   - 工作 goroutine 通过 atomic 扣减令牌，令牌不足时通过 sync.Cond.Wait 休眠。
//
// 工作 goroutine 在有令牌时仅执行 1 次 atomic Add，零锁争用。
type QPSController struct {
	tokens    atomic.Int64 // 当前可用令牌（以 1/tokenScale 为单位，避免浮点损失）
	targetQPS atomic.Int64 // 目标 QPS * tokenScale（整数化），<= 0 表示不限速

	mu     sync.Mutex
	cond   *sync.Cond // 用于 Broadcast 唤醒所有等待中的 worker
	stopCh chan struct{}
	once   sync.Once
}

// tokenScale 将浮点 QPS 转为整数运算的精度因子。
// 1 token = tokenScale 内部单位。
const tokenScale int64 = 1_000_000

// maxBurstTokens 令牌上限 = 2 * tokenScale（即最多累积 2 个未消费令牌）
const maxBurstTokens = 2 * tokenScale

// refillerInterval refiller 协程的补充间隔
const refillerInterval = time.Millisecond

func NewQPSController(targetQPS float64) *QPSController {
	q := &QPSController{
		stopCh: make(chan struct{}),
	}
	q.cond = sync.NewCond(&q.mu)
	q.targetQPS.Store(int64(targetQPS * float64(tokenScale)))
	q.tokens.Store(tokenScale) // 初始 1 个令牌
	go q.refiller()
	return q
}

// refiller 独立协程：按固定间隔补充令牌。
func (q *QPSController) refiller() {
	ticker := time.NewTicker(refillerInterval)
	defer ticker.Stop()

	lastTime := time.Now()
	for {
		select {
		case <-q.stopCh:
			// 确保所有阻塞在 Wait 的 worker 被唤醒
			q.cond.Broadcast()
			return
		case now := <-ticker.C:
			target := q.targetQPS.Load()
			if target <= 0 {
				lastTime = now
				continue
			}
			elapsed := now.Sub(lastTime)
			lastTime = now

			// 补充量 = targetQPS * elapsed（已整数化）
			add := target * elapsed.Nanoseconds() / int64(time.Second)
			if add <= 0 {
				continue
			}

			cur := q.tokens.Add(add)
			// 限制上限
			if cur > maxBurstTokens {
				q.tokens.Add(-(cur - maxBurstTokens))
			}

			// Broadcast 唤醒所有等待中的 worker
			q.cond.Broadcast()
		}
	}
}

// Acquire 阻塞直到获得一个令牌。工作协程的唯一调用路径。
func (q *QPSController) Acquire() {
	if q.targetQPS.Load() <= 0 {
		return // 不限速
	}
	for {
		cur := q.tokens.Add(-tokenScale)
		if cur >= 0 {
			return // 拿到令牌
		}
		// 令牌不足，回退扣减
		q.tokens.Add(tokenScale)
		// 通过 sync.Cond 等待 refiller Broadcast 唤醒
		q.mu.Lock()
		for q.tokens.Load() < tokenScale {
			q.cond.Wait()
		}
		q.mu.Unlock()
	}
}

// SetQPS 运行时动态调整 QPS（由 PressureController 调用，发生在 refiller 之外）。
func (q *QPSController) SetQPS(qps float64) {
	q.targetQPS.Store(int64(qps * float64(tokenScale)))
}

// CurrentQPS 返回当前目标 QPS
func (q *QPSController) CurrentQPS() float64 {
	return float64(q.targetQPS.Load()) / float64(tokenScale)
}

// Stop 停止 refiller goroutine。应在使用完毕后调用。
func (q *QPSController) Stop() {
	q.once.Do(func() { close(q.stopCh) })
}
