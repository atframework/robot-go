package atsf4g_go_robot_user

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
	log "github.com/atframework/atframe-utils-go/log"
	pu "github.com/atframework/atframe-utils-go/proto_utility"
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

type MessageHandlerFunc func(*TaskActionUser, *pu.LazyUnmarshalProtobufMessage, int32) error

type User interface {
	IsLogin() bool
	Login()
	Logout()

	AllocSequence() uint64
	ReceiveHandler(unpack UserReceiveUnpackFunc, createMsg UserReceiveCreateMessageFunc)
	SendReq(action base.TaskActionImpl, csMsg proto.Message, csHead proto.Message,
		csBody proto.Message, rpcName string, sequence uint64, needRsp bool) (int32, *pu.LazyUnmarshalProtobufMessage, error)

	TakeActionGuard(task base.TaskActionImpl) error
	ReleaseActionGuard(task base.TaskActionImpl) error
	IsTakenActionGuard(task base.TaskActionImpl) bool

	AwaitReceiveHandlerClose(task base.TaskActionImpl) error

	RunTask(timeout time.Duration, f func(*TaskActionUser) error, name string) *TaskActionUser
	RunTaskDefaultTimeout(f func(*TaskActionUser) error, name string) *TaskActionUser
	RunTaskWithoutLock(timeout time.Duration, f func(*TaskActionUserNoneLock) error, name string) *TaskActionUserNoneLock

	AddOnClosedHandler(f func(User))
	Log(format string, a ...any)
	InitHeartbeatFunc(func(User) error)

	GetLogined() bool
	GetOpenId() string
	GetAccessToken() string
	GetUserId() uint64
	GetZoneId() uint32

	SetUserId(uint64)
	SetZoneId(uint32)
	SetHeartbeatInterval(time.Duration)
	SetLastPingTime(time.Time)
	SetHasGetInfo(bool)
	RegisterMessageHandler(rpcName string, f MessageHandlerFunc)

	GetExtralData(key string) any
	SetExtralData(key string, value any)
}

type CreateUserFuncType func(openId string, logHandler func(format string, a ...any),
	enableActorLog bool, lazyUnmarshal bool, unpack UserReceiveUnpackFunc, createMsg UserReceiveCreateMessageFunc,
	connectFn conn.NewConnectFunc) User

var createUserFn func(openId string, logHandler func(format string, a ...any), enableActorLog bool, lazyUnmarshal bool, connectFn conn.NewConnectFunc) User

func RegisterCreateUser(f CreateUserFuncType,
	unpack UserReceiveUnpackFunc, createMsg UserReceiveCreateMessageFunc) {
	createUserFn = func(openId string, logHandler func(format string, a ...any), enableActorLog bool, lazyUnmarshal bool, connectFn conn.NewConnectFunc) User {
		return f(openId, logHandler, enableActorLog, lazyUnmarshal, unpack, createMsg, connectFn)
	}
}

func CreateUser(openId string, logHandler func(format string, a ...any), enableActorLog bool, lazyUnmarshal bool) User {
	if createUserFn == nil {
		return nil
	}
	return createUserFn(openId, logHandler, enableActorLog, lazyUnmarshal, base.ConnectFunc)
}

var loginUserCount atomic.Int64

// OnlineUserCount 返回当前在线用户数
func OnlineUserCount() int {
	return int(loginUserCount.Load())
}

func CreateDefaultUserLogHandler(openId string) func(format string, a ...any) {
	logBufferWriter, _ := log.NewLogBufferedRotatingWriter(nil,
		fmt.Sprintf("../log/user/%s.%%N.log", openId), "", 5*1024*1024, 1, time.Second*3, 0)
	return func(format string, a ...any) {
		fmt.Fprintf(logBufferWriter, "%s %s", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, a...))
	}
}

type UserHolder struct {
	user   *lu.AtomicInterface[User]
	OpenId string
	Index  atomic.Int32
}

func (h *UserHolder) InitUser(u User) {
	h.user = lu.NewAtomicInterface(u)
}

func (h *UserHolder) GetUser() User {
	if h == nil {
		return nil
	}
	return h.user.Load()
}

func (h *UserHolder) IsUserVaildLogin() bool {
	return h != nil && !lu.IsNil(h.GetUser()) && h.GetUser().IsLogin()
}

var userMapContainer = sync.Map{}

func OnUserLogin() {
	loginUserCount.Add(1)
}

func OnUserLogout() {
	loginUserCount.Add(-1)
}

func UserContainerGetUser(openId string) *UserHolder {
	v, _ := userMapContainer.LoadOrStore(openId, &UserHolder{
		OpenId: openId,
	})
	return v.(*UserHolder)
}

func UserContainerTryGetUser(openId string) *UserHolder {
	v, _ := userMapContainer.Load(openId)
	if v == nil {
		return nil
	}
	return v.(*UserHolder)
}

func UserContainerDelUser(holder *UserHolder) {
	userMapContainer.CompareAndDelete(holder.GetUser().GetOpenId(), holder)
}

// LogoutAllUsers 登出并清理所有在线用户
func LogoutAllUsers() {
	userMapContainer.Range(func(_, value any) bool {
		holder := value.(*UserHolder)
		if lu.IsNil(holder.GetUser()) {
			return true
		}
		holder.GetUser().Logout()
		return true
	})
	userMapContainer.Clear()
}

func GetAllUsers() []User {
	var users []User
	userMapContainer.Range(func(_, value any) bool {
		holder := value.(*UserHolder)
		if lu.IsNil(holder.GetUser()) {
			return true
		}
		users = append(users, holder.GetUser())
		return true
	})
	return users
}
