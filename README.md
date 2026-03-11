# robot-go

通用的 Go 语言机器人测试客户端框架，提供 WebSocket 连接管理、交互式命令行、任务调度和批量用例执行等基础设施，用于模拟用户与服务器的交互。

## 模块结构

```
robot-go/
├── robot.go            # 框架入口，提供 NewRobotFlagSet() 和 StartRobot()
├── base/
│   ├── config.go       # 全局配置（SocketUrl 等）
│   └── task_action.go  # 任务执行框架（TaskActionImpl / TaskActionBase / TaskActionManager）
├── case/
│   └── action.go       # 批量用例执行框架（RegisterCase / RunCaseFile）
├── cmd/
│   └── user.go         # 用户管理与命令路由（RegisterUserCommand / GetCurrentUser）
├── data/
│   ├── user.go         # User 接口定义及工厂注册
│   ├── action.go       # TaskActionUser（带用户上下文的任务）
│   └── impl/
│       └── user.go     # User 接口实现（WebSocket / RPC / 心跳）
└── utils/
    ├── readline.go     # 交互式命令行（RegisterCommand / 自动补全 / 命令树）
    └── history.go      # 命令历史管理
```

## 快速开始

### 安装

```bash
go get github.com/atframework/robot-go
```

### 最小使用示例

```go
package main

import (
    "os"

    "google.golang.org/protobuf/proto"

    robot "github.com/atframework/robot-go"
)

func UnpackMessage(msg proto.Message) (rpcName string, typeName string, errorCode int32,
    msgHead proto.Message, bodyBin []byte, sequence uint64, err error) {
    // 从服务端返回的消息中解析出 rpcName、errorCode、sequence 等字段
    // ...
    return
}

func main() {
    flagSet := robot.NewRobotFlagSet()
    // 可添加自定义 flag
    // flagSet.String("resource", "", "resource directory")

    if err := flagSet.Parse(os.Args[1:]); err != nil {
        return
    }

    robot.StartRobot(flagSet, UnpackMessage, func() proto.Message {
        // 返回服务端消息的 protobuf 外层包装类型
        return &YourCSMsg{}
    })
}
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-url` | `ws://localhost:7001/ws/v1` | 服务器 WebSocket 地址 |
| `-case_file` | 空 | 用例配置文件路径，指定后自动执行用例而非进入交互模式 |
| `-h` / `-help` | | 显示帮助信息 |

## 核心概念

### User 接口

`data.User` 定义了与服务器交互的用户抽象，主要能力：

- **连接管理**: WebSocket 连接、登录/登出、心跳
- **RPC 通信**: `SendReq()` 发送请求并等待响应，通过 sequence 匹配
- **推送消息**: `RegisterMessageHandler()` 注册服务端主动推送的消息处理器
- **任务执行**: `RunTask()` / `RunTaskDefaultTimeout()` 在用户上下文中执行异步任务
- **扩展数据**: `GetExtralData()` / `SetExtralData()` 存储自定义数据

使用框架时需要实现两个回调函数并传入 `StartRobot()`：

```go
// UserReceiveUnpackFunc - 解析服务端返回的消息
type UserReceiveUnpackFunc func(proto.Message) (
    rpcName string, typeName string, errorCode int32,
    msgHead proto.Message, bodyBin []byte, sequence uint64, err error)

// UserReceiveCreateMessageFunc - 创建服务端消息的 protobuf 实例
type UserReceiveCreateMessageFunc func() proto.Message
```

### 任务系统 (TaskAction)

任务基于 channel 的 Yield/Resume 模式实现协作式调度：

- `TaskActionBase`: 基础任务实现，支持超时控制、任务链（`AwaitTask`）
- `TaskActionUser`: 绑定 User 上下文的任务，RPC 等待期间自动释放 User 操作锁
- `TaskActionCase`: 用例任务，用于批量执行场景
- `TaskActionManager`: 管理任务生命周期（分配 ID、超时计时器、等待完成）

```go
// 在用户上下文中执行任务
user.RunTaskDefaultTimeout(func(action *user_data.TaskActionUser) error {
    errCode, rsp, err := protocol.SomeRpc(action)
    if err != nil {
        return err
    }
    // 处理响应...
    return nil
}, "Task Name")
```

### 命令注册

通过 `utils.RegisterCommand()` 或 `cmd.RegisterUserCommand()` 注册交互式命令：

```go
func init() {
    // 注册普通命令（无需当前用户上下文）
    utils.RegisterCommandDefaultTimeout(
        []string{"user", "login"},  // 命令路径
        LoginCmd,                    // 处理函数
        "<openid>",                  // 参数说明
        "登录协议",                   // 命令描述
        nil,                         // 自动补全函数
    )

    // 注册用户命令（自动注入当前用户）
    cmd.RegisterUserCommand(
        []string{"user", "getInfo"},
        GetInfoCmd,
        "",
        "拉取用户信息",
        nil,
    )
}
```

交互模式下输入 `help` 可查看所有已注册命令。命令支持 Tab 自动补全。

### 用例系统 (Case)

用例系统用于批量自动化测试，支持并发执行和进度显示。

#### 注册用例

```go
func init() {
    robot_case.RegisterCase("login", LoginCase, time.Second*5)
    robot_case.RegisterCase("logout", LogoutCase, time.Second*5)
}

func LoginCase(action *robot_case.TaskActionCase, openId string, args []string) error {
    u := user_data.CreateUser(openId, base.SocketUrl, action.Log, false)
    if u == nil {
        return fmt.Errorf("failed to create user")
    }

    err := action.AwaitTask(u.RunTaskDefaultTimeout(LoginTask, "Login Task"))
    if err != nil {
        return err
    }
    return nil
}
```

#### 用例配置文件

通过 `-case_file` 指定配置文件自动执行用例，格式为：

```
<case_name> <openid_prefix> <user_count> <batch_count> <iterations> [args...] [&]
```

| 字段 | 说明 |
|------|------|
| `case_name` | 已注册的用例名称 |
| `openid_prefix` | 用户 OpenID 前缀，自动追加序号 0~(user_count-1) |
| `user_count` | 模拟用户数量 |
| `batch_count` | 最大并发数 |
| `iterations` | 每个用户执行次数 |
| `args` | 传递给用例的额外参数 |
| `&` | 行尾加 `&` 表示异步执行，不等待完成即执行下一行 |

以 `#` 开头的行为注释。示例：

```conf
# 登录
login 1250001 60 60 1
# 并发 GetInfo 测试
run_cmd 1250001 60 60 1 user getInfo &
run_cmd 1250001 60 60 1 user getInfo &
run_cmd 1250001 60 60 1 user getInfo
# 登出
logout 1250001 60 60 1
```

#### 交互模式执行用例

```
run-case <case_name> <openid_prefix> <user_count> <batch_count> <iterations> [args...]
```

## 完整使用示例

以下展示一个完整的项目结构（参考实际游戏服务器测试客户端）：

```
your-robot/
├── main.go                # 入口：初始化配置、实现 UnpackMessage、调用 StartRobot
├── go.mod
├── case/
│   └── basic_case.go      # 注册用例：login, logout, gm, delay_second 等
├── case_config/
│   └── simple_test.conf   # 用例配置文件
├── cmd/
│   ├── user.go            # 用户命令：login, logout, getInfo, ping, gm
│   ├── adventure.go       # 业务命令：冒险相关
│   └── ...                # 更多业务命令
├── protocol/
│   ├── user.go            # RPC 封装：LoginAuthRpc, LoginRpc, GetInfoRpc
│   ├── rpc_handle.go      # 生成的 RPC 处理器（Send* / RegisterMessageHandler*）
│   └── ...                # 更多 RPC 封装
└── task/
    └── user.go            # 任务定义：LoginTask, LogoutTask, PingTask
```

### main.go 示例

```go
package main

import (
    "fmt"
    "os"

    "google.golang.org/protobuf/proto"

    _ "your-project/case"      // 通过 init() 自动注册用例
    _ "your-project/cmd"       // 通过 init() 自动注册命令
    robot "github.com/atframework/robot-go"
)

func UnpackMessage(msg proto.Message) (rpcName string, typeName string, errorCode int32,
    msgHead proto.Message, bodyBin []byte, sequence uint64, err error) {
    csMsg, ok := msg.(*YourCSMsg)
    if !ok {
        err = fmt.Errorf("message type invalid: %T", msg)
        return
    }
    // 从 csMsg 中提取 rpcName, errorCode, bodyBin, sequence 等
    return
}

func main() {
    flagSet := robot.NewRobotFlagSet()
    if err := flagSet.Parse(os.Args[1:]); err != nil {
        fmt.Println(err)
        return
    }

    robot.StartRobot(flagSet, UnpackMessage, func() proto.Message {
        return &YourCSMsg{}
    })
}
```

### task 示例

```go
func LoginTask(task *user_data.TaskActionUser) error {
    // 1. 登录认证
    errCode, rsp, err := protocol.LoginAuthRpc(task)
    if err != nil {
        return err
    }

    user := task.User
    user.SetLoginCode(rsp.GetLoginCode())
    user.SetUserId(rsp.GetUserId())

    // 2. 登录
    errCode, loginRsp, err := protocol.LoginRpc(task)
    if err != nil {
        return err
    }

    user.SetLogined(true)
    user.SetHeartbeatInterval(time.Duration(loginRsp.GetHeartbeatInterval()) * time.Second)
    user.InitHeartbeatFunc(PingTask)
    return nil
}
```

## 运行

```bash
# 交互模式
go run . -url ws://localhost:7001/ws/v1

# 执行用例文件
go run . -url ws://localhost:7001/ws/v1 -case_file case_config/simple_test.conf
```

## License

[MIT License](LICENSE)