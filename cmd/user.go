package atsf4g_go_robot_cmd

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	base "github.com/atframework/robot-go/base"
	user_data "github.com/atframework/robot-go/data"
	utils "github.com/atframework/robot-go/utils"
	"github.com/chzyer/readline"
)

func init() {
	utils.RegisterCommandDefaultTimeout([]string{"user", "show_all_login_user"}, func(action base.TaskActionImpl, cmd []string) string {
		userMapLock.Lock()
		defer userMapLock.Unlock()
		for _, v := range userMapContainer {
			action.Log("%d", v.GetUserId())
		}
		return ""
	}, "", "显示所有登录User", nil)
	utils.RegisterCommandDefaultTimeout([]string{"user", "switch"}, func(action base.TaskActionImpl, cmd []string) string {
		if len(cmd) < 1 {
			return "Need User Id"
		}

		userId, err := strconv.ParseInt(cmd[0], 10, 64)
		if err != nil {
			return err.Error()
		}

		userMapLock.Lock()
		v, ok := userMapContainer[strconv.FormatUint(uint64(userId), 10)]
		userMapLock.Unlock()
		if !ok {
			return "not found user"
		}

		SetCurrentUser(v)
		return ""
	}, "<userId>", "切换登录User", AutoCompleteUseIdWithoutCurrent)
}

var currentUser atomic.Pointer[user_data.User]

func GetCurrentUser() user_data.User {
	ret := currentUser.Load()
	if ret == nil {
		return nil
	}

	return *ret
}

func SetCurrentUser(user user_data.User) {
	if user == nil {
		currentUser.Store(nil)
	} else {
		currentUser.Store(&user)
	}

	rlInst := utils.GetCurrentReadlineInstance()
	if rlInst != nil {
		if user != nil {
			rlInst.SetPrompt("\033[32m" + strconv.FormatUint(user.GetUserId(), 10) + " »\033[0m ")
			rlInst.Refresh()
		} else {
			rlInst.SetPrompt("\033[32m»\033[0m ")
			rlInst.Refresh()
		}
	}
}

var (
	userMapContainer map[string]user_data.User
	userMapLock      sync.Mutex
)

func MutableUserMapContainer() map[string]user_data.User {
	if userMapContainer == nil {
		userMapContainer = make(map[string]user_data.User)
	}
	return userMapContainer
}

func AutoCompleteUseId(string) []string {
	userMapLock.Lock()
	defer userMapLock.Unlock()
	var res []string
	for _, k := range userMapContainer {
		res = append(res, strconv.FormatUint(k.GetUserId(), 10))
	}
	return res
}

func AutoCompleteUseIdWithoutCurrent(string) []string {
	userMapLock.Lock()
	defer userMapLock.Unlock()
	var res []string
	for _, k := range userMapContainer {
		if k.GetUserId() == GetCurrentUser().GetUserId() {
			continue
		}
		res = append(res, strconv.FormatUint(k.GetUserId(), 10))
	}
	return res
}

func CurrentUserRunTaskDefaultTimeout(f func(*user_data.TaskActionUser) error, name string) *user_data.TaskActionUser {
	user := GetCurrentUser()
	if user == nil {
		utils.StdoutLog("GetCurrentUser: User nil")
		return nil
	}
	return user.RunTaskDefaultTimeout(f, name)
}

type UserCommandFunc func(base.TaskActionImpl, user_data.User, []string) string

type UserCommandNode struct {
	Children map[string]*UserCommandNode
	Func     UserCommandFunc
}

var root *UserCommandNode

func MutableUserCommandRoot() *UserCommandNode {
	if root != nil {
		return root
	}
	root = &UserCommandNode{Children: make(map[string]*UserCommandNode)}
	return root
}

func RegisterUserCommand(path []string, fn UserCommandFunc, argsInfo string, desc string,
	dynamicComplete readline.DynamicCompleteFunc) {
	utils.RegisterCommandDefaultTimeout(path, func(action base.TaskActionImpl, cmd []string) string {
		user := GetCurrentUser()
		if user == nil {
			return "GetCurrentUser: User nil"
		}
		fn(action, user, cmd)
		return ""
	}, argsInfo, desc, dynamicComplete)

	current := MutableUserCommandRoot()
	for _, key := range path {
		if current.Children[key] == nil {
			current.Children[key] = &UserCommandNode{
				Children: make(map[string]*UserCommandNode),
			}
		}
		current = current.Children[key]
	}
	current.Func = fn
}

func GetUserCommandFunc(path []string) ([]string, UserCommandFunc) {
	current := MutableUserCommandRoot()
	ret := path
	for _, key := range path {
		if current.Children[key] == nil {
			break
		}
		current = current.Children[key]
		ret = ret[1:]
	}
	return ret, current.Func
}

func LogoutAllUsers() {
	userMapLock.Lock()
	userMapContainerCopy := userMapContainer
	userMapContainer = make(map[string]user_data.User)
	userMapLock.Unlock()

	for _, v := range userMapContainerCopy {
		v.Logout()
	}
	for _, v := range userMapContainerCopy {
		v.AwaitReceiveHandlerClose()
	}
}

func CmdCreateUser(action base.TaskActionImpl, openId string) (user_data.User, error) {
	userMapLock.Lock()
	if existingUser, ok := MutableUserMapContainer()[openId]; ok && existingUser != nil {
		userMapLock.Unlock()
		return nil, fmt.Errorf("User already logged in")
	}
	userMapLock.Unlock()

	// 创建角色
	u := user_data.CreateUser(openId, base.SocketUrl, action.Log, true)
	if u == nil {
		return nil, fmt.Errorf("Failed to create user")
	}

	userMapLock.Lock()
	MutableUserMapContainer()[openId] = u
	userMapLock.Unlock()

	u.AddOnClosedHandler(func(user user_data.User) {
		userMapLock.Lock()
		defer userMapLock.Unlock()

		u, ok := MutableUserMapContainer()[openId]
		if !ok || u != user {
			return
		}
		delete(MutableUserMapContainer(), openId)
		user.Log("Remove User: %s", openId)

		if GetCurrentUser() == user {
			SetCurrentUser(nil)
		}
	})
	return u, nil
}
