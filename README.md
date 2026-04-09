# robot-go

通用的 Go 语言机器人测试客户端框架，提供 WebSocket 连接管理、交互式命令行、任务调度和批量用例执行等基础设施，用于模拟用户与服务器的交互。

## 模块结构

```
robot-go/
├── robot.go            # 框架入口，提供 NewRobotFlagSet() 和 StartRobot()
├── solo.go             # Solo 单节点压测模式（写 Redis + 本地生成 HTML）
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
    return
}

func main() {
    flagSet := robot.NewRobotFlagSet()
    if err := flagSet.Parse(os.Args[1:]); err != nil {
        return
    }

    robot.StartRobot(flagSet, UnpackMessage, func() proto.Message {
        return &YourCSMsg{}
    })
}
```

## 运行模式

robot-go 支持三种运行模式：

| 模式 | `-mode` | 说明 |
|------|---------|------|
| Standalone | 空（默认） | 交互式命令行，或通过 `-case_file` 执行普通用例 |
| Solo | `solo` | 单节点压测，数据写入 Redis，当前目录生成 HTML 报告 |
| Agent | `agent` | 分布式 Agent，连接 Master 接收任务，结果写入 Redis |

### Standalone 模式

```bash
# 交互模式
go run . -url ws://localhost:7001/ws/v1

# 执行普通用例文件
go run . -url ws://localhost:7001/ws/v1 -case_file case_config/simple_test.conf
```

### Solo 单节点压测模式

Solo 模式适用于自动化 CI/CD 流程或单机压测场景。需要 Redis，数据同时写入 Redis（Master 可查看）并在当前目录生成 HTML 报告。

```bash
go run . -mode solo \
    -url ws://localhost:7001/ws/v1 \
    -redis-addr localhost:6379 \
    -case_file benchmark.conf \
    -case_file_repeated 3
```

执行流程：

1. 连接 Redis，通过 INCR 生成唯一 ReportID（或使用 `-report-id` 指定）
2. 解析 `#!stress` 格式 case 文件，逐行顺序执行（支持 `case_file_repeated` 多轮）
3. 每个 case 运行完整的 QPS 控制、自适应压力检测与打点采集
4. 全部 case 完成后，将 tracings 和 metrics 写入 Redis
5. 在当前目录生成 `{reportID}.html`（自包含 ECharts 报告，浏览器直接打开）

由于数据写入 Redis，Master 的 Web Dashboard 和 API 同样可以查询和查看 Solo 的报告。

### 分布式压测（Master / Agent）

当单节点无法产生足够压力时，可使用 Master/Agent 模式将压测任务分发到多台机器执行，Redis 作为数据汇聚中间件。

```
┌──────────┐       HTTP API       ┌──────────────┐
│  Master  │ ←─────────────────→ │   Agent 1    │
│  :8080   │                      ├──────────────┤
│  Redis   │ ←─  写入 Tracings  ─ │   Agent 2    │
└──────────┘                      └──────────────┘
```

**1. 启动 Master：**

```bash
# 预编译二进制（无需业务 protobuf 依赖，仅需 Redis）
go build -o robot-master ./bin/
./robot-master -listen :8080 -redis-addr localhost:6379 -report-dir ./report
```

**2. 在每台压测机上启动 Agent：**

```bash
go run . -mode agent \
    -master-addr http://192.168.1.10:8080 \
    -redis-addr 192.168.1.10:6379 \
    -url ws://192.168.1.2:7001/ws/v1 \
    -agent-id agent-01
```

**3. 提交压测任务：**

```bash
curl -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "case_file_content": "#!stress\nlogin_bench false test_ 1 1001 50 50 60",
    "repeated_time": 1,
    "report_id": ""
  }'
```

`report_id` 为空时，Master 会通过 Redis INCR 自动生成唯一 ID。

#### 一键部署 Master

Master 提供独立预编译二进制，可通过部署脚本快速下载。

**Linux / macOS：**

```bash
curl -fsSL https://raw.githubusercontent.com/atframework/robot-go/main/deploy-master.sh | bash
cd robot-master && ./robot-master -config master.yaml
```

**Windows PowerShell：**

```powershell
irm https://raw.githubusercontent.com/atframework/robot-go/main/deploy-master.ps1 -OutFile deploy-master.ps1
.\deploy-master.ps1
cd robot-master; .\robot-master.exe -config master.yaml
```

部署脚本自动检测平台、从 GitHub Releases 下载对应二进制和默认配置 `master.yaml`。

支持的平台：`linux-amd64`、`linux-arm64`、`darwin-amd64`、`darwin-arm64`、`windows-amd64`。

> 对仓库打 `v*` Tag 后 GitHub Actions 自动构建并发布到 Releases。

#### Master HTTP API

| Method | Path | 说明 |
|--------|------|------|
| `POST` | `/api/agents/register` | Agent 注册（Agent 启动时自动调用） |
| `GET` | `/api/agents` | 查询已注册的 Agent 列表及状态 |
| `POST` | `/api/agents/reboot` | 重启指定 Agent（或全部） |
| `POST` | `/api/tasks` | 提交压测任务（返回 `report_id`，异步执行） |
| `GET` | `/api/tasks/{id}` | 查询任务状态（`pending/running/done/error`） |
| `GET` | `/api/tasks/all` | 列出所有内存中的任务 |
| `GET` | `/api/tasks/history` | 任务历史（持久化在 Redis） |
| `POST` | `/api/tasks/{id}/stop` | 停止运行中的任务 |
| `GET` | `/api/reports` | 列出所有报告 |
| `POST` | `/api/reports/{id}/html` | 从 Redis 数据重新生成 HTML 报告 |
| `DELETE` | `/api/reports/{id}` | 删除报告（Redis + 本地文件） |
| `GET` | `/reports/{id}/view` | 在浏览器中查看 HTML 报告 |

## 命令行参数

### 通用参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-config` | `config.yaml` | YAML 配置文件路径（所有参数均可写入 YAML，命令行优先） |
| `-url` | `ws://localhost:7001/ws/v1` | 服务器 WebSocket 地址 |
| `-connect-type` | `websocket` | 连接类型：`websocket`、`atgateway` |
| `-access-token` | 空 | atgateway 模式：认证 Token |
| `-key-exchange` | `none` | atgateway 模式：ECDH 算法（`none`/`x25519`/`p256`/…） |
| `-crypto` | `none` | atgateway 模式：加密算法（`none`/`aes-128-gcm`/`chacha20`/…） |
| `-compression` | `none` | atgateway 模式：压缩算法（`none`/`zstd`/`lz4`/`snappy`/`zlib`） |
| `-case_file` | 空 | 用例配置文件路径（支持普通用例文件和 `#!stress` 压测文件） |
| `-case_file_repeated` | `1` | 用例文件执行次数 |

### 模式与分布式参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-mode` | 空（standalone） | 运行模式：空=单机交互，`solo`=单节点压测，`agent`=分布式 Agent |
| `-redis-addr` | `localhost:6379` | Redis 地址（solo 和 agent 模式必需） |
| `-redis-pwd` | 空 | Redis 密码 |
| `-report-id` | 空（自动生成） | 报告 ID；为空时通过 Redis INCR 自动生成唯一 ID |
| `-master-addr` | 空 | Master HTTP 地址（agent 模式必填） |
| `-agent-id` | 空（自动生成） | Agent 唯一 ID |
| `-agent-group` | 空 | Agent 组 ID，Master 可按组分发任务 |

### Master 独有参数（`bin/master_main.go`）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-listen` | `:8080` | HTTP 监听地址 |
| `-report-dir` | `./report` | HTML 报告及 JSON 备份输出目录 |
| `-report-expiry` | 空（永不过期） | 报告自动过期时长（如 `168h` = 7 天） |

所有参数均可通过 YAML 配置文件指定（去掉前缀 `-`），命令行参数优先级更高。

## YAML 配置文件

通过 `-config` 指定（默认自动读取当前目录下的 `config.yaml`）：

```yaml
# solo 模式示例
mode: solo
url: ws://192.168.1.2:7001/ws/v1
redis-addr: 192.168.1.10:6379
case_file: benchmark.conf
case_file_repeated: 3
```

```yaml
# Master 配置示例 (master.yaml)
listen: ":8080"
redis-addr: "localhost:6379"
redis-pwd: ""
report-dir: "./report"
# report-expiry: 168h
```

## ReportID 唯一性

无论 Solo 还是 Master，默认 ReportID 都通过 Redis key `report:id:seq` 执行 INCR 操作生成，格式为 `{timestamp}-{seq}`（如 `20250610-143052-42`）。这保证了并发提交任务时 ID 不会冲突。

也可通过 `-report-id` 或 API 的 `report_id` 字段手动指定。

## 核心概念

### User 接口

`data.User` 定义了与服务器交互的用户抽象：

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
    utils.RegisterCommandDefaultTimeout(
        []string{"user", "login"},
        LoginCmd,
        "<openid>",
        "登录协议",
        nil,
    )

    cmd.RegisterUserCommand(
        []string{"user", "getInfo"},
        GetInfoCmd,
        "",
        "拉取用户信息",
        nil,
    )
}
```

交互模式下输入 `help` 可查看所有已注册命令，支持 Tab 自动补全。

### 用例系统 (Case)

#### 注册用例

```go
func init() {
    robot_case.RegisterCase("login", LoginCase, time.Second*5)
}

func LoginCase(action *robot_case.TaskActionCase, openId string, args []string) error {
    u := user_data.CreateUser(openId, base.SocketUrl, action.Log, false)
    if u == nil {
        return fmt.Errorf("failed to create user")
    }
    err := action.AwaitTask(u.RunTaskDefaultTimeout(LoginTask, "Login Task"))
    return err
}
```

#### 普通用例配置文件

通过 `-case_file` 指定，格式：

```
<case_name> <openid_prefix> <user_count> <batch_count> <iterations> [args...] [&]
```

| 字段 | 说明 |
|------|------|
| `case_name` | 已注册的用例名称 |
| `openid_prefix` | 用户 OpenID 前缀，自动追加序号 |
| `user_count` | 模拟用户数量 |
| `batch_count` | 最大并发数 |
| `iterations` | 每个用户执行次数 |
| `&` | 行尾加 `&` 表示异步执行 |

以 `#` 开头的行为注释。示例：

```conf
# 登录
login 1250001 60 60 1
# 并发 GetInfo 测试
run_cmd 1250001 60 60 1 user getInfo &
run_cmd 1250001 60 60 1 user getInfo
# 登出
logout 1250001 60 60 1
```

#### 压测用例配置文件（`#!stress`）

文件首行为 `#!stress` 时切换到压测模式（Solo 和 Agent 模式使用）。每行定义一个压测用例：

```
<case_name> <error_break> <openid_prefix> <id_start> <id_end> <batch_count> <target_qps> <run_time> [args...]
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `case_name` | string | 已注册的用例名称 |
| `error_break` | bool | `true`/`false`：遇到错误是否立即停止 |
| `openid_prefix` | string | 账号 ID 前缀 |
| `id_start` | int64 | 账号起始编号（含） |
| `id_end` | int64 | 账号结束编号（不含） |
| `batch_count` | int64 | 最大并发数 |
| `target_qps` | float64 | 目标 QPS，`0` 表示不限速 |
| `run_time` | int64 | 持续时长（秒），`0` 表示每账号只执行一次 |
| `args` | strings | 额外透传参数 |

示例（`benchmark.conf`）：

```conf
#!stress
# caseName   errorBreak  prefix    start  end    batch  qps   runTime
login_bench  false       test_     1      1001   50     50    60
query_bench  false       test_     1      1001   100    100   60
```

#### QPS 令牌桶行为

压测模式使用连续时间浮点令牌桶，动态计算等待间隔：

- 令牌按连续时间累积：`tokensToAdd = elapsed.Seconds() × targetQPS`
- 等待间隔根据缺额精确计算：`waitTime = (1 - tokens) / targetQPS`，最小休眠 1ms
- 桶容量上限 1.5 个令牌，避免突发
- `target_qps=60` 可精确达到 60 QPS

#### 交互模式执行用例

```
run-case <case_name> <openid_prefix> <user_count> <batch_count> <iterations> [args...]
run-case-stress <caseName> <errorBreak> <openIdPrefix> <idStart> <idEnd> <batchCount> <targetQPS> <runTime> [args...]
```

## 报告系统

### 数据存储

所有压测数据（tracings、metrics、meta）均通过 Redis 持久化：

| Redis Key | 类型 | 说明 |
|-----------|------|------|
| `report:meta:{reportID}` | String | 报告元数据（JSON） |
| `report:tracing:{reportID}:{agentID}` | List | 打点记录（JSON 分块） |
| `report:metrics:{reportID}:{agentID}` | List | 指标数据（JSON 分块） |
| `report:index` | SortedSet | 报告索引（member=reportID, score=unix） |
| `report:id:seq` | String | 全局 ReportID 自增序列 |

### HTML 报告

生成的 HTML 文件为自包含 ECharts 报告，浏览器直接打开即可查看。内容包含：

- 分用例统计表（Total / Avg / P50 / P90 / P99 / Min / Max / 成功率）
- 每个用例的 QPS 曲线
- 延迟分布曲线（P50/P90/P99）
- 成功/失败趋势
- 错误码分布饼图
- 在线用户及自定义 Metrics 曲线

**Solo 模式**：HTML 生成在当前目录，文件名为 `{reportID}.html`。

**Master 模式**：HTML 生成在 `{report-dir}/{reportID}/html`，可通过 Web Dashboard 查看或通过 API 重新生成。

## 完整项目示例

```
your-robot/
├── main.go                # 入口：实现 UnpackMessage、调用 StartRobot
├── go.mod
├── case/
│   └── basic_case.go      # 注册用例：login, logout 等
├── case_config/
│   └── benchmark.conf     # 压测配置文件
├── cmd/
│   └── user.go            # 用户命令
├── protocol/
│   └── user.go            # RPC 封装
└── task/
    └── user.go            # 任务定义
```

### main.go

```go
package main

import (
    "fmt"
    "os"

    "google.golang.org/protobuf/proto"

    _ "your-project/case"
    _ "your-project/cmd"
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

### 任务示例

```go
func LoginTask(task *user_data.TaskActionUser) error {
    errCode, rsp, err := protocol.LoginAuthRpc(task)
    if err != nil {
        return err
    }

    user := task.User
    user.SetLoginCode(rsp.GetLoginCode())
    user.SetUserId(rsp.GetUserId())

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

## License

[MIT License](LICENSE)