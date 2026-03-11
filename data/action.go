package atsf4g_go_robot_user

import (
	base "github.com/atframework/robot-go/base"
)

type TaskActionUser struct {
	base.TaskActionBase
	User User
	Fn   func(*TaskActionUser) error
}

func init() {
	var _ base.TaskActionImpl = &TaskActionUser{}
}

func (t *TaskActionUser) BeforeYield() {
	t.User.ReleaseActionGuard()
}

func (t *TaskActionUser) AfterYield() {
	t.User.TakeActionGuard()
}

func (t *TaskActionUser) HookRun() error {
	t.User.TakeActionGuard()
	defer t.User.ReleaseActionGuard()
	return t.Fn(t)
}

func (t *TaskActionUser) Log(format string, a ...any) {
	t.User.Log(format, a...)
}
