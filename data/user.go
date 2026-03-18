package atsf4g_go_robot_user

import (
	"sync"
	"time"

	base "github.com/atframework/robot-go/base"
	conn "github.com/atframework/robot-go/conn"
	"google.golang.org/protobuf/proto"
)

type UserReceiveUnpackFunc func(proto.Message) (
	rpcName string,
	typeName string,
	errorCode int32,
	msgHead proto.Message,
	bodyBin []byte,
	sequence uint64,
	err error)
type UserReceiveCreateMessageFunc func() proto.Message

type User interface {
	IsLogin() bool
	Logout()
	AllocSequence() uint64
	ReceiveHandler(unpack UserReceiveUnpackFunc, createMsg UserReceiveCreateMessageFunc)
	SendReq(action *TaskActionUser, csMsg proto.Message, csHead proto.Message,
		csBody proto.Message, rpcName string, sequence uint64, needRsp bool) (int32, proto.Message, error)
	TakeActionGuard()
	ReleaseActionGuard()
	RunTask(timeout time.Duration, f func(*TaskActionUser) error, name string) *TaskActionUser
	RunTaskDefaultTimeout(f func(*TaskActionUser) error, name string) *TaskActionUser
	AddOnClosedHandler(f func(User))
	Log(format string, a ...any)
	AwaitReceiveHandlerClose()
	InitHeartbeatFunc(func(User) error)

	GetLoginCode() string
	GetLogined() bool
	GetOpenId() string
	GetAccessToken() string
	GetUserId() uint64
	GetZoneId() uint32

	SetLoginCode(string)
	SetUserId(uint64)
	SetZoneId(uint32)
	SetLogined(bool)
	SetHeartbeatInterval(time.Duration)
	SetLastPingTime(time.Time)
	SetHasGetInfo(bool)
	RegisterMessageHandler(rpcName string, f func(*TaskActionUser, proto.Message, int32) error)

	GetExtralData(key string) any
	SetExtralData(key string, value any)
}

type CreateUserFuncType func(openId string, logHandler func(format string, a ...any),
	enableActorLog bool, unpack UserReceiveUnpackFunc, createMsg UserReceiveCreateMessageFunc,
	connectFn conn.NewConnectFunc) User

var createUserFn func(openId string, logHandler func(format string, a ...any), enableActorLog bool, connectFn conn.NewConnectFunc) User

func RegisterCreateUser(f CreateUserFuncType,
	unpack UserReceiveUnpackFunc, createMsg UserReceiveCreateMessageFunc) {
	createUserFn = func(openId string, logHandler func(format string, a ...any), enableActorLog bool, connectFn conn.NewConnectFunc) User {
		return f(openId, logHandler, enableActorLog, unpack, createMsg, connectFn)
	}
}

func CreateUser(openId string, logHandler func(format string, a ...any), enableActorLog bool) User {
	if createUserFn == nil {
		return nil
	}
	return createUserFn(openId, logHandler, enableActorLog, base.ConnectFunc)
}

var userMapContainerLock sync.RWMutex
var userMapContainer = make(map[string]User)

func UserContainerAddUser(u User) {
	userMapContainerLock.Lock()
	defer userMapContainerLock.Unlock()

	userMapContainer[u.GetOpenId()] = u
	u.AddOnClosedHandler(func(user User) {
		UserContainerDelUser(user.GetOpenId(), user)
	})
}

func UserContainerDelUser(openId string, checkUser User) {
	userMapContainerLock.Lock()
	defer userMapContainerLock.Unlock()

	v, ok := userMapContainer[openId]
	if !ok {
		return
	}
	if v == checkUser {
		delete(userMapContainer, openId)
	}
}

func UserContainerGetUser(openId string) User {
	userMapContainerLock.RLock()
	defer userMapContainerLock.RUnlock()

	v, ok := userMapContainer[openId]
	if !ok {
		return nil
	}
	return v
}
