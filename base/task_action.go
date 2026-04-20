package atsf4g_go_robot_protocol_base

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
	"github.com/panjf2000/ants/v2"
)

type TaskActionImpl interface {
	AwaitTask(TaskActionImpl) error
	InitOnFinish(func(TaskActionImpl, error))
	GetTaskId() uint64
	BeforeYield() error
	AfterYield() error
	Finish(error)
	InitTaskId(uint64)
	GetTimeoutDuration() time.Duration
	InitTimeoutTimer(*time.Timer)
	TimeoutKill()
	Kill()
	IsTakenActionGuard() bool

	Yield() *TaskActionResumeData
	Resume(*TaskActionAwaitData, *TaskActionResumeData)

	HookRun() error
	SetAwaitData(awaitData TaskActionAwaitData)
	GetAwaitData() TaskActionAwaitData
	ClearAwaitData()

	Log(format string, a ...any)
}

func AwaitTask(other TaskActionImpl) error {
	AwaitChannel := make(chan TaskActionResumeData, 1)
	other.InitOnFinish(func(task TaskActionImpl, err error) {
		AwaitChannel <- TaskActionResumeData{
			Err:  err,
			Data: nil,
		}
	})
	resumeData := <-AwaitChannel
	return resumeData.Err
}

const (
	TaskActionAwaitTypeNone = iota
	TaskActionAwaitTypeNormal
	TaskActionAwaitTypeRPC
)

type TaskActionAwaitData struct {
	WaitingType uint32
	WaitingId   uint64
}

type TaskActionResumeData struct {
	Err  error
	Data interface{}
}

type TaskActionBase struct {
	Impl   TaskActionImpl
	Name   string
	TaskId uint64

	AwaitData       TaskActionAwaitData
	AwaitChannel    chan *TaskActionResumeData
	timeoutDuration time.Duration
	Timeout         *time.Timer

	finishLock sync.Mutex
	finished   atomic.Bool
	kill       atomic.Bool
	result     error
	onFinish   []func(TaskActionImpl, error)
}

func NewTaskActionBase(timeoutDuration time.Duration, name string) *TaskActionBase {
	t := &TaskActionBase{
		timeoutDuration: timeoutDuration,
		Name:            name,
	}
	return t
}

func (t *TaskActionBase) SetAwaitData(awaitData TaskActionAwaitData) {
	t.AwaitData = awaitData
	if t.AwaitChannel == nil {
		t.AwaitChannel = make(chan *TaskActionResumeData, 1)
		if t.GetTimeoutDuration() > 0 {
			timeoutTimer := time.AfterFunc(t.GetTimeoutDuration(), func() {
				t.TimeoutKill()
			})
			t.InitTimeoutTimer(timeoutTimer)
		}
	}
}

func (t *TaskActionBase) GetAwaitData() TaskActionAwaitData {
	return t.AwaitData
}

func (t *TaskActionBase) ClearAwaitData() {
	t.AwaitData = TaskActionAwaitData{}
}

// 需要提前 SetAwaitData
func (t *TaskActionBase) Yield() *TaskActionResumeData {
	if t.kill.Load() {
		return &TaskActionResumeData{
			Err: fmt.Errorf("task action killed"),
		}
	}
	if t.AwaitChannel == nil {
		return &TaskActionResumeData{
			Err: fmt.Errorf("task action not set await data"),
		}
	}
	if !t.Impl.IsTakenActionGuard() {
		return &TaskActionResumeData{
			Err: fmt.Errorf("task action guard not taken"),
		}
	}
	err := t.Impl.BeforeYield()
	if err != nil {
		return &TaskActionResumeData{
			Err: err,
		}
	}
	ret := <-t.AwaitChannel
	t.AwaitData = TaskActionAwaitData{}
	err = t.Impl.AfterYield()
	if err != nil {
		return &TaskActionResumeData{
			Err: err,
		}
	}
	return ret
}

func (t *TaskActionBase) Resume(awaitData *TaskActionAwaitData, resumeData *TaskActionResumeData) {
	if t.AwaitChannel == nil {
		return
	}
	if t.AwaitData.WaitingId == awaitData.WaitingId && t.AwaitData.WaitingType == awaitData.WaitingType {
		t.AwaitChannel <- resumeData
	}
}

func (t *TaskActionBase) TimeoutKill() {
	if t.finished.Load() {
		return
	}
	if t.AwaitChannel == nil {
		return
	}
	t.kill.Store(true)
	t.Impl.Log("task timeout killed %s", t.Name)
	if t.AwaitData.WaitingId != 0 && t.AwaitData.WaitingType != TaskActionAwaitTypeNone {
		t.AwaitChannel <- &TaskActionResumeData{
			Err: fmt.Errorf("sys timeout"),
		}
	} else {
		t.Finish(fmt.Errorf("sys timeout"))
	}
}

func (t *TaskActionBase) Kill() {
	if t.finished.Load() {
		return
	}
	if t.AwaitChannel == nil {
		return
	}
	t.kill.Store(true)
	t.Impl.Log("task killed %s", t.Name)
	if t.AwaitData.WaitingId != 0 && t.AwaitData.WaitingType != TaskActionAwaitTypeNone {
		t.AwaitChannel <- &TaskActionResumeData{
			Err: fmt.Errorf("killed"),
		}
	} else {
		t.Finish(fmt.Errorf("killed"))
	}
}

func (t *TaskActionBase) Finish(result error) {
	if t.Timeout != nil {
		t.Timeout.Stop()
	}
	if result != nil {
		t.Impl.Log("Finish %s with error: %v", t.Name, result)
	}
	if t.finished.CompareAndSwap(false, true) {
		t.finishLock.Lock()
		defer t.finishLock.Unlock()

		t.result = result
		for index := len(t.onFinish) - 1; index >= 0; index-- {
			t.onFinish[index](t.Impl, t.result)
		}
		t.onFinish = t.onFinish[:0]
	}
}

func (t *TaskActionBase) InitOnFinish(fn func(TaskActionImpl, error)) {
	if t.finished.Load() {
		fn(t.Impl, t.result)
		return
	}
	t.finishLock.Lock()
	defer t.finishLock.Unlock()
	if t.finished.Load() {
		fn(t.Impl, t.result)
		return
	} else {
		t.onFinish = append(t.onFinish, fn)
	}
}

func (t *TaskActionBase) GetTaskId() uint64 {
	return t.TaskId
}

func (t *TaskActionBase) AwaitTask(other TaskActionImpl) error {
	if lu.IsNil(other) {
		return fmt.Errorf("task nil")
	}
	if t.kill.Load() {
		return fmt.Errorf("task action killed")
	}
	// 先设置等待数据，再注册回调，避免回调先于设置等待数据导致无法正确唤醒
	t.SetAwaitData(TaskActionAwaitData{
		WaitingType: TaskActionAwaitTypeNormal,
		WaitingId:   other.GetTaskId(),
	})
	other.InitOnFinish(func(task TaskActionImpl, err error) {
		t.Resume(&TaskActionAwaitData{
			WaitingType: TaskActionAwaitTypeNormal,
			WaitingId:   other.GetTaskId(),
		}, &TaskActionResumeData{
			Err:  err,
			Data: nil,
		})
	})
	resumeData := t.Yield()
	return resumeData.Err
}

func (t *TaskActionBase) BeforeYield() error {
	// do nothing
	return nil
}

func (t *TaskActionBase) AfterYield() error {
	// do nothing
	return nil
}

func (t *TaskActionBase) IsTakenActionGuard() bool {
	return true
}

func (t *TaskActionBase) InitTaskId(id uint64) {
	t.TaskId = id
	t.finished.Store(false)
	t.result = nil
	t.kill.Store(false)
}

// ResetForReuse 重置任务状态以便复用（不重新分配 AwaitChannel）。
// 适用于压测场景中的任务对象重用。
func (t *TaskActionBase) ResetForReuse() {
	t.finished.Store(false)
	t.result = nil
	t.kill.Store(false)
	t.AwaitData = TaskActionAwaitData{}
	t.onFinish = t.onFinish[:0]
	if t.Timeout != nil {
		t.Timeout.Stop()
		t.Timeout = nil
	}
	// 排空 AwaitChannel 中的残留数据
	if t.AwaitChannel != nil {
		select {
		case <-t.AwaitChannel:
		default:
		}
	}
}

func (t *TaskActionBase) GetTimeoutDuration() time.Duration {
	return t.timeoutDuration
}

func (t *TaskActionBase) InitTimeoutTimer(timer *time.Timer) {
	t.Timeout = timer
}

type TaskActionManager struct {
	wg sync.WaitGroup

	// 池模式
	workerPool *ants.PoolWithFunc

	// 普通模式
	activeMu sync.Mutex
	active   map[uint64]TaskActionImpl
}

var taskIdAlloc atomic.Uint64

func init() {
	taskIdAlloc.Store(
		uint64(time.Since(time.Unix(1577836800, 0)).Nanoseconds()))
}

func NewTaskActionManager() *TaskActionManager {
	ret := &TaskActionManager{
		active: make(map[uint64]TaskActionImpl),
	}
	return ret
}

// NewTaskActionManagerWithPool 创建使用 channel-based 协程池的 TaskActionManager。
// poolSize 即 worker 数量和 channel 缓冲大小。
func NewTaskActionManagerWithPool(poolSize int) *TaskActionManager {
	if poolSize <= 0 {
		poolSize = 256
	}
	ret := &TaskActionManager{}
	ret.workerPool, _ = ants.NewPoolWithFunc(poolSize, func(i any) {
		if task, ok := i.(TaskActionImpl); ok {
			task.Finish(task.HookRun())
		}
	},
		ants.WithPanicHandler(func(a any) {
			panic(a)
		}),
	)
	return ret
}

// ReleasePool 关闭 worker goroutine。
func (m *TaskActionManager) ReleasePool() {
	if m.workerPool != nil {
		m.workerPool.Release()
	}
}

func (m *TaskActionManager) allocTaskId() uint64 {
	return taskIdAlloc.Add(1)
}

func (m *TaskActionManager) WaitAll() {
	m.wg.Wait()
}

func (m *TaskActionManager) CloseAll() {
	if m.active == nil {
		return
	}
	m.activeMu.Lock()
	tasks := make([]TaskActionImpl, 0, len(m.active))
	for _, t := range m.active {
		tasks = append(tasks, t)
	}
	m.activeMu.Unlock()
	for _, t := range tasks {
		t.Kill()
	}
}

func (m *TaskActionManager) OnTaskFinish(taskId uint64) {
	if m.active != nil {
		m.activeMu.Lock()
		delete(m.active, taskId)
		m.activeMu.Unlock()
	}
	m.wg.Done()
}

func (m *TaskActionManager) RunTaskAction(taskAction TaskActionImpl) {
	taskAction.InitTaskId(m.allocTaskId())

	if m.active != nil {
		m.activeMu.Lock()
		m.active[taskAction.GetTaskId()] = taskAction
		m.activeMu.Unlock()
	}

	m.wg.Add(1)
	if m.workerPool != nil {
		m.workerPool.Invoke(taskAction)
	} else {
		go func() {
			taskAction.Finish(taskAction.HookRun())
		}()
	}
}
