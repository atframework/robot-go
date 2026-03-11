package atsf4g_go_robot_protocol_base

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
)

type TaskActionImpl interface {
	AwaitTask(TaskActionImpl) error
	InitOnFinish(func(error))
	GetTaskId() uint64
	BeforeYield()
	AfterYield()
	Finish(error)
	InitTaskId(uint64)
	GetTimeoutDuration() time.Duration
	InitTimeoutTimer(*time.Timer)
	TimeoutKill()
	Kill()
	HookRun() error
	Log(format string, a ...any)
}

func AwaitTask(other TaskActionImpl) error {
	AwaitChannel := make(chan TaskActionResumeData, 1)
	other.InitOnFinish(func(err error) {
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

	awaitData       TaskActionAwaitData
	AwaitChannel    chan *TaskActionResumeData
	timeoutDuration time.Duration
	Timeout         *time.Timer

	finishLock sync.Mutex
	finished   bool
	kill       atomic.Bool
	result     error
	onFinish   []func(error)
}

func NewTaskActionBase(timeoutDuration time.Duration, name string) *TaskActionBase {
	t := &TaskActionBase{
		timeoutDuration: timeoutDuration,
		Name:            name,
		AwaitChannel:    make(chan *TaskActionResumeData),
	}
	return t
}

func (t *TaskActionBase) Yield(awaitData TaskActionAwaitData) *TaskActionResumeData {
	if t.kill.Load() {
		return &TaskActionResumeData{
			Err: fmt.Errorf("task action killed"),
		}
	}
	t.awaitData = awaitData
	t.Impl.BeforeYield()
	ret := <-t.AwaitChannel
	t.awaitData.WaitingId = 0
	t.awaitData.WaitingType = 0
	t.Impl.AfterYield()
	return ret
}

func (t *TaskActionBase) Resume(awaitData *TaskActionAwaitData, resumeData *TaskActionResumeData) {
	if t.awaitData.WaitingId == awaitData.WaitingId && t.awaitData.WaitingType == awaitData.WaitingType {
		t.AwaitChannel <- resumeData
	}
}

func (t *TaskActionBase) TimeoutKill() {
	if t.finished {
		return
	}
	t.kill.Store(true)
	t.Impl.Log("task timeout killed %s", t.Name)
	if t.awaitData.WaitingId != 0 && t.awaitData.WaitingType != TaskActionAwaitTypeNone {
		t.AwaitChannel <- &TaskActionResumeData{
			Err: fmt.Errorf("sys timeout"),
		}
	}
}

func (t *TaskActionBase) Kill() {
	if t.finished {
		return
	}
	t.kill.Store(true)
	t.Impl.Log("task killed %s", t.Name)
	if t.awaitData.WaitingId != 0 && t.awaitData.WaitingType != TaskActionAwaitTypeNone {
		t.AwaitChannel <- &TaskActionResumeData{
			Err: fmt.Errorf("killed"),
		}
	}
}

func (t *TaskActionBase) Finish(result error) {
	if t.Timeout != nil {
		t.Timeout.Stop()
	}
	t.Impl.Log("Finish %s", t.Name)
	t.finishLock.Lock()
	defer t.finishLock.Unlock()
	t.finished = true
	t.result = result
	for _, fn := range t.onFinish {
		fn(t.result)
	}
}

func (t *TaskActionBase) InitOnFinish(fn func(error)) {
	t.finishLock.Lock()
	defer t.finishLock.Unlock()
	if t.finished {
		fn(t.result)
		return
	}
	t.onFinish = append(t.onFinish, fn)
}

func (t *TaskActionBase) GetTaskId() uint64 {
	return t.TaskId
}

func (t *TaskActionBase) AwaitTask(other TaskActionImpl) error {
	if lu.IsNil(other) {
		return fmt.Errorf("task nil")
	}
	other.InitOnFinish(func(err error) {
		t.Resume(&TaskActionAwaitData{
			WaitingType: TaskActionAwaitTypeNormal,
			WaitingId:   other.GetTaskId(),
		}, &TaskActionResumeData{
			Err:  err,
			Data: nil,
		})
	})
	resumeData := t.Yield(TaskActionAwaitData{
		WaitingType: TaskActionAwaitTypeNormal,
		WaitingId:   other.GetTaskId(),
	})
	return resumeData.Err
}

func (t *TaskActionBase) BeforeYield() {
	// do nothing
}

func (t *TaskActionBase) AfterYield() {
	// do nothing
}

func (t *TaskActionBase) InitTaskId(id uint64) {
	t.TaskId = id
	t.finished = false
	t.result = nil
	t.kill.Store(false)
}

func (t *TaskActionBase) GetTimeoutDuration() time.Duration {
	return t.timeoutDuration
}

func (t *TaskActionBase) InitTimeoutTimer(timer *time.Timer) {
	t.Timeout = timer
}

type TaskActionManager struct {
	taskIdMap   sync.Map
	taskIdAlloc atomic.Uint64
}

func NewTaskActionManager() *TaskActionManager {
	ret := &TaskActionManager{}
	ret.taskIdAlloc.Store(
		uint64(time.Since(time.Unix(1577836800, 0)).Nanoseconds()))
	return ret
}

func (m *TaskActionManager) allocTaskId() uint64 {
	id := m.taskIdAlloc.Add(1)
	return id
}

func (m *TaskActionManager) WaitAll() {
	AllTask := []TaskActionImpl{}
	m.taskIdMap.Range(func(key, value any) bool {
		AllTask = append(AllTask, value.(TaskActionImpl))
		return true
	})
	for _, taskAction := range AllTask {
		_ = AwaitTask(taskAction)
	}
}

func (m *TaskActionManager) CloseAll() {
	AllTask := []TaskActionImpl{}
	m.taskIdMap.Range(func(key, value any) bool {
		AllTask = append(AllTask, value.(TaskActionImpl))
		return true
	})
	for _, taskAction := range AllTask {
		taskAction.Kill()
	}
}

func (m *TaskActionManager) RunTaskAction(taskAction TaskActionImpl) {
	taskAction.InitTaskId(m.allocTaskId())
	m.taskIdMap.Store(taskAction.GetTaskId(), taskAction)

	if taskAction.GetTimeoutDuration() > 0 {
		timeoutTimer := time.AfterFunc(taskAction.GetTimeoutDuration(), func() {
			taskAction.TimeoutKill()
		})
		taskAction.InitTimeoutTimer(timeoutTimer)
	}
	go func() {
		taskAction.Finish(taskAction.HookRun())
		m.taskIdMap.Delete(taskAction.GetTaskId())
	}()
}
