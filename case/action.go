package atsf4g_go_robot_case

import (
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
	base "github.com/atframework/robot-go/base"
	user "github.com/atframework/robot-go/data"
	report "github.com/atframework/robot-go/report"
)

type CaseFunc func(*TaskActionCase, *user.UserHolder, []string) error

type TaskActionCase struct {
	base.TaskActionBase
	Fn           CaseFunc
	logHandler   func(openId string, format string, a ...any)
	UserHolder   *user.UserHolder
	TracerEntry  report.TracingEntry
	DispatchedAt time.Time // 记录任务分发时间，用于计算延迟
	Args         []string
	NeedLog      bool
}

func (t *TaskActionCase) HookRun() error {
	if !lu.IsNil(t.UserHolder.GetUser()) {
		err := t.UserHolder.GetUser().TakeActionGuard(t)
		if err != nil {
			return err
		}
	}
	err := t.Fn(t, t.UserHolder, t.Args)
	if t.TracerEntry != nil {
		t.TracerEntry.EndWithError(err)
		t.TracerEntry = nil
	}
	if !lu.IsNil(t.UserHolder.GetUser()) {
		err := t.UserHolder.GetUser().ReleaseActionGuard(t)
		if err != nil {
			return err
		}
	}
	return err
}

func (t *TaskActionCase) InitUserOnRunning(u user.User) error {
	t.UserHolder.InitUser(u)
	return t.UserHolder.GetUser().TakeActionGuard(t)
}

func (t *TaskActionCase) IsTakenActionGuard() bool {
	if !lu.IsNil(t.UserHolder.GetUser()) {
		return t.UserHolder.GetUser().IsTakenActionGuard(t)
	}
	return true
}

func (t *TaskActionCase) BeforeYield() error {
	if !lu.IsNil(t.UserHolder.GetUser()) {
		return t.UserHolder.GetUser().ReleaseActionGuard(t)
	}
	return nil
}

func (t *TaskActionCase) AfterYield() error {
	if !lu.IsNil(t.UserHolder.GetUser()) {
		return t.UserHolder.GetUser().TakeActionGuard(t)
	}
	return nil
}

func (t *TaskActionCase) Log(format string, a ...any) {
	if t.logHandler != nil {
		t.logHandler(t.UserHolder.OpenId, format, a...)
	}
}

func init() {
	var _ base.TaskActionImpl = &TaskActionCase{}
}

type CaseAction struct {
	fun     CaseFunc
	timeout time.Duration
}

var caseMapContainer = make(map[string]CaseAction)

func RegisterCase(name string, fn CaseFunc, timeout time.Duration) {
	caseMapContainer[name] = CaseAction{
		fun:     fn,
		timeout: timeout,
	}
}

func AutoCompleteCaseName(string) []string {
	var res []string
	for k := range caseMapContainer {
		res = append(res, k)
	}
	return res
}
