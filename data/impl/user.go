package atsf4g_go_robot_user_impl

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/atframework/atframe-utils-go/log"
	pu "github.com/atframework/atframe-utils-go/proto_utility"
	base "github.com/atframework/robot-go/base"
	conn "github.com/atframework/robot-go/conn"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	user_data "github.com/atframework/robot-go/data"
)

type User struct {
	OpenId string
	UserId uint64
	ZoneId uint32

	AccessToken string
	LoginCode   string

	Logined           bool
	HasGetInfo        bool
	HeartbeatInterval time.Duration
	LastPingTime      time.Time
	Closed            atomic.Bool

	connectionSequence uint64
	connection         conn.Connection
	rpcAwaitTask       *user_data.RPCRingBuffer
	sendBuf            []byte // 复用的 proto.Marshal 缓冲区，避免每次分配

	csLog *log.LogBufferedRotatingWriter

	onClosed                []func(user user_data.User)
	taskManager             *base.TaskActionManager
	taskActionGuard         sync.Mutex
	logHandler              func(format string, a ...any)
	receiveHandlerCloseChan chan struct{}

	messageHandler map[string]func(*user_data.TaskActionUser, proto.Message, int32) error

	extralData map[string]any
}

type CmdAction struct {
	cmdFn           func(user user_data.User)
	allowedNotLogin bool
}

func init() {
	var _ user_data.User = &User{}
}

func NewUser(openId string, c conn.Connection, bufferWriter *log.LogBufferedRotatingWriter, logHandler func(format string, a ...any)) *User {
	var _ user_data.User = &User{}
	ret := &User{
		OpenId:                  openId,
		UserId:                  0,
		ZoneId:                  1,
		AccessToken:             fmt.Sprintf("access-token-for-%s", openId),
		connectionSequence:      99,
		connection:              c,
		rpcAwaitTask:            user_data.NewRPCRingBuffer(256),
		csLog:                   bufferWriter,
		taskManager:             base.NewTaskActionManager(),
		messageHandler:          make(map[string]func(*user_data.TaskActionUser, proto.Message, int32) error),
		logHandler:              logHandler,
		receiveHandlerCloseChan: make(chan struct{}, 1),
	}

	var _ user_data.User = ret
	return ret
}

func CreateUser(openId string, logHandler func(format string, a ...any),
	enableActorLog bool, unpack user_data.UserReceiveUnpackFunc, createMsg user_data.UserReceiveCreateMessageFunc, connectFn conn.NewConnectFunc) user_data.User {
	var bufferWriter *log.LogBufferedRotatingWriter
	if enableActorLog {
		bufferWriter, _ = log.NewLogBufferedRotatingWriter(nil,
			fmt.Sprintf("../log/actor/%s.%%N.log", openId), "", 20*1024*1024, 3, time.Second*3, 0)
	}
	c, err := connectFn()
	if err != nil {
		if logHandler != nil {
			logHandler("Error connecting to server: %v", err)
		}
		return nil
	}

	ret := NewUser(openId, c, bufferWriter, logHandler)
	go ret.ReceiveHandler(unpack, createMsg)

	ret.Log("Create User")
	return ret
}

func (u *User) AddOnClosedHandler(f func(user user_data.User)) {
	if f == nil {
		return
	}
	u.onClosed = append(u.onClosed, f)
}

func (u *User) IsLogin() bool {
	if u == nil {
		return false
	}
	if u.Closed.Load() {
		return false
	}
	if !u.Logined {
		return false
	}
	return true
}

func (user *User) Logout() {
	if !user.IsLogin() {
		return
	}
	user.Log("user logout")
	user.Logined = false
	user_data.OnUserLogout()
	user.Close()
}

func (user *User) Login() {
	if user == nil {
		return
	}
	if user.IsLogin() {
		return
	}
	user.Log("user Login")
	user.Logined = true
	user_data.OnUserLogin()
}

func (u *User) TakeActionGuard() {
	u.taskActionGuard.Lock()
}

func (u *User) ReleaseActionGuard() {
	u.taskActionGuard.Unlock()
}

func (user *User) AllocSequence() uint64 {
	user.connectionSequence++
	return user.connectionSequence
}

func (user *User) RunTask(timeout time.Duration, f func(*user_data.TaskActionUser) error, name string) *user_data.TaskActionUser {
	if user == nil {
		user.Log("User nil")
		return nil
	}
	task := &user_data.TaskActionUser{
		TaskActionBase: *base.NewTaskActionBase(timeout, name),
		User:           user,
		Fn:             f,
	}
	task.TaskActionBase.Impl = task

	user.taskManager.RunTaskAction(task)
	return task
}

func (user *User) RunTaskDefaultTimeout(f func(*user_data.TaskActionUser) error, name string) *user_data.TaskActionUser {
	return user.RunTask(time.Duration(8)*time.Second, f, name)
}

type rpcResumeData struct {
	body    proto.Message
	rspCode int32
}

func (user *User) ReceiveHandler(unpack user_data.UserReceiveUnpackFunc, createMsg user_data.UserReceiveCreateMessageFunc) {
	defer func() {
		user.Log("connection closed.")
		user.RunTaskDefaultTimeout(func(action *user_data.TaskActionUser) error {
			user.connection = nil
			user.Close()
			user.receiveHandlerCloseChan <- struct{}{}
			action.InitOnFinish(func(base.TaskActionImpl, error) {
				user.taskManager.CloseAll()
			})
			return nil
		}, "ReceiveHandler Close")
	}()
	for {
		bytes, err := user.connection.ReadMessage()
		if err != nil {
			if user.connection.IsUnexpectedCloseError(err) {
				user.Log("Error in receive: %v", err)
			}
			return
		}

		Msg := createMsg()
		err = proto.Unmarshal(bytes, Msg)
		if err != nil {
			user.Log("Error in Unmarshal: %v", err)
			return
		}

		var rpcName string
		var typeName string
		var errorCode int32
		var csMsgHead proto.Message
		var bodyBin []byte
		var sequence uint64

		rpcName, typeName, errorCode, csMsgHead, bodyBin, sequence, err = unpack(Msg)
		if err != nil {
			user.Log("<<<<<<<<<<<<<<<<<<<< Received: Error In Unpack %v <<<<<<<<<<<<<<<<<<<<", err)
			user.Log("Msg: %v MsgHead: %s", Msg, prototext.Format(csMsgHead))
			continue
		}

		// errorCode = csMsg.Head.ErrorCode
		// csMsgHead = csMsg.Head
		// bodyBin = csMsg.BodyBin
		// sequence = csMsg.Head.ClientSequence

		// switch csMsg.Head.GetRpcType().(type) {
		// case *public_protocol_extension.CSMsgHead_RpcResponse:
		// 	rpcName = csMsg.Head.GetRpcResponse().GetRpcName()
		// 	typeName = csMsg.Head.GetRpcResponse().GetTypeUrl()
		// case *public_protocol_extension.CSMsgHead_RpcStream:
		// 	rpcName = csMsg.Head.GetRpcStream().GetRpcName()
		// 	typeName = csMsg.Head.GetRpcStream().GetTypeUrl()
		// default:
		// 	user.Log("<<<<<<<<<<<<<<<<<<<< Received: Unsupport RpcType <<<<<<<<<<<<<<<<<<<<")
		// 	user.Log("%s", prototext.Format(csMsgHead))
		// 	continue
		// }

		if user.logHandler != nil {
			user.Log("User: %d Code: %d <<<<<<<<<<<<<<<< Received: %s <<<<<<<<<<<<<<<<<<<", user.GetUserId(), errorCode, rpcName)
		}
		messageType, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(typeName))
		if err != nil {
			if user.logHandler != nil {
				user.Log("Unsupport in TypeName: %s ", typeName)
			}
			if user.csLog != nil {
				fmt.Fprintf(user.csLog, "%s %s\nHead:%s", time.Now().Format("2006-01-02 15:04:05.000"),
					fmt.Sprintf("<<<<<<<<<<<<<<<<<<<< Unsupport Received: %s <<<<<<<<<<<<<<<<<<<", rpcName), pu.MessageReadableTextIndent(csMsgHead))
			}
			continue
		}
		csBody := messageType.New().Interface()

		err = proto.Unmarshal(bodyBin, csBody)
		if err != nil {
			if user.logHandler != nil {
				user.Log("Error in Unmarshal: %v", err)
			}
			if user.csLog != nil {
				fmt.Fprintf(user.csLog, "%s %s\nHead:%s", time.Now().Format("2006-01-02 15:04:05.000"),
					fmt.Sprintf("<<<<<<<<<<<<<<<<<<<< Unmarshal Error Received: %s <<<<<<<<<<<<<<<<<<<", rpcName), pu.MessageReadableTextIndent(csMsgHead))
			}
			return
		}

		if user.csLog != nil {
			fmt.Fprintf(user.csLog, "%s %s\nHead:%s\nBody:%s", time.Now().Format("2006-01-02 15:04:05.000"),
				fmt.Sprintf("<<<<<<<<<<<<<<<<<<<< Received: %s <<<<<<<<<<<<<<<<<<<", rpcName), pu.MessageReadableTextIndent(csMsgHead), pu.MessageReadableTextIndent(csBody))
		}
		task, ok := user.rpcAwaitTask.LoadAndDelete(sequence)
		if ok {
			// RPC response
			task.Resume(&base.TaskActionAwaitData{
				WaitingType: base.TaskActionAwaitTypeRPC,
				WaitingId:   sequence,
			}, &base.TaskActionResumeData{
				Err: nil,
				Data: rpcResumeData{
					body:    csBody,
					rspCode: errorCode,
				},
			})
		} else {
			// SYNC
			f, ok := user.messageHandler[rpcName]
			if ok && f != nil {
				user.RunTaskDefaultTimeout(func(tau *user_data.TaskActionUser) error {
					return f(tau, csBody, errorCode)
				}, rpcName)
			}
		}
	}
}

func (user *User) AwaitReceiveHandlerClose() {
	user.ReleaseActionGuard()
	<-user.receiveHandlerCloseChan
	user.TakeActionGuard()
}

func (user *User) InitHeartbeatFunc(f func(user_data.User) error) {
	if !user.IsLogin() {
		return
	}
	if user.LastPingTime.Add(user.HeartbeatInterval).Before(time.Now()) {
		err := f(user)
		if err != nil {
			user.Log("Heartbeat error stop check")
			return
		}
	}
	time.AfterFunc(5*time.Second, func() {
		user.InitHeartbeatFunc(f)
	})
}

type RpcTimeout struct {
	sendTime time.Time
	rpcName  string
	seq      uint64
}

func (user *User) SendReq(action base.TaskActionImpl, csMsg proto.Message,
	csHead proto.Message, csBody proto.Message, rpcName string, sequence uint64, needRsp bool) (int32, proto.Message, error) {
	if user == nil {
		return 0, nil, fmt.Errorf("no login")
	}

	if user.connection == nil || !user.connection.IsValid() {
		return 0, nil, fmt.Errorf("connection not found")
	}

	if user.Closed.Load() {
		return 0, nil, fmt.Errorf("connection lost")
	}

	// 复用 sendBuf 避免每次 Marshal 分配新 []byte
	var err2 error
	user.sendBuf, err2 = proto.MarshalOptions{}.MarshalAppend(user.sendBuf[:0], csMsg)
	if err2 != nil {
		return 0, nil, fmt.Errorf("marshal error: %w", err2)
	}
	csBin := user.sendBuf
	if user.logHandler != nil {
		titleString := fmt.Sprintf("User: %d >>>>>>>>>>>>>>>>>>>> Sending: %s >>>>>>>>>>>>>>>>>>>>", user.GetUserId(), rpcName)
		user.Log("%s", titleString)
		if user.csLog != nil {
			fmt.Fprintf(user.csLog, "%s %s\nHead:%s\nBody:%s", time.Now().Format("2006-01-02 15:04:05.000"),
				titleString, pu.MessageReadableTextIndent(csHead), pu.MessageReadableTextIndent(csBody))
		}
	}

	if needRsp {
		awaitData := base.TaskActionAwaitData{
			WaitingType: base.TaskActionAwaitTypeRPC,
			WaitingId:   sequence,
		}
		action.SetAwaitData(awaitData)
		user.rpcAwaitTask.StoreBlocking(sequence, action)
	}

	err := user.connection.WriteMessage(csBin)
	if err != nil {
		user.Log("Error during writing to websocket: %v", err)
		if needRsp {
			awaitData := action.GetAwaitData()
			if awaitData.WaitingId == sequence && awaitData.WaitingType == base.TaskActionAwaitTypeRPC {
				action.ClearAwaitData()
			}
			user.rpcAwaitTask.Delete(sequence)
		}
		return 0, nil, err
	}

	if needRsp {
		resumeData := action.Yield()
		if resumeData.Err != nil {
			return 0, nil, resumeData.Err
		}
		data := resumeData.Data.(rpcResumeData)
		return data.rspCode, data.body, nil
	}
	return 0, nil, nil
}

func (user *User) Close() {
	if user.Closed.CompareAndSwap(false, true) {
		for _, f := range user.onClosed {
			f(user)
		}
		if user.connection != nil {
			err := user.connection.Close()
			if err != nil {
				user.Log("Error during closing connection: %v", err)
				return
			}
		}
	}
}

func (user *User) RegisterMessageHandler(rpcName string, f func(*user_data.TaskActionUser, proto.Message, int32) error) {
	user.messageHandler[rpcName] = f
}

func (user *User) Log(format string, a ...any) {
	if user == nil || user.logHandler == nil {
		return
	}
	user.logHandler(format, a...)
}

func (user *User) GetLogined() bool {
	if user == nil {
		return false
	}
	return user.Logined
}

func (user *User) GetOpenId() string {
	if user == nil {
		return ""
	}
	return user.OpenId
}

func (user *User) GetAccessToken() string {
	if user == nil {
		return ""
	}
	return user.AccessToken
}

func (user *User) GetUserId() uint64 {
	if user == nil {
		return 0
	}
	return user.UserId
}

func (user *User) GetZoneId() uint32 {
	if user == nil {
		return 0
	}
	return user.ZoneId
}

func (user *User) SetUserId(d uint64) {
	if user == nil {
		return
	}
	user.UserId = d
}

func (user *User) SetZoneId(d uint32) {
	if user == nil {
		return
	}
	user.ZoneId = d
}

func (user *User) SetHeartbeatInterval(d time.Duration) {
	if user == nil {
		return
	}
	user.HeartbeatInterval = d
}

func (user *User) SetLastPingTime(d time.Time) {
	if user == nil {
		return
	}
	user.LastPingTime = d
}

func (user *User) SetHasGetInfo(d bool) {
	if user == nil {
		return
	}
	user.HasGetInfo = d
}

func (user *User) GetExtralData(key string) any {
	if user == nil {
		return nil
	}
	return user.extralData[key]
}

func (user *User) SetExtralData(key string, value any) {
	if user == nil {
		return
	}
	if user.extralData == nil {
		user.extralData = make(map[string]any)
	}
	user.extralData[key] = value
}
