package atsf4g_go_robot_case

// Params 压测运行参数，由 Master 分发给 Agent 时携带
type Params struct {
	// 用例名称
	CaseName string `json:"case_name"`
	// 解析 case 文件时的行号，从 0 开始
	CaseIndex int `json:"case_index"`
	// 错误是否打断：true 时遇到第一个错误即停止整个Case
	ErrorBreak bool `json:"error_break"`

	// 账号 ID 范围（半开区间）: [OpenIDStart, OpenIDEnd)
	OpenIDPrefix string `json:"openid_prefix"`
	OpenIDStart  int64  `json:"openid_start"`
	OpenIDEnd    int64  `json:"openid_end"`

	// QPS 控制：目标 QPS，0 表示不限速
	TargetQPS float64 `json:"target_qps"`

	// 单账号并发度
	UserBatchCount int64 `json:"user_batch_count"`

	// 运行持续时间（秒），0 表示每个账号只跑一次
	RunTime int64 `json:"run_time"`

	// 额外透传参数
	ExtraArgs []string `json:"extra_args,omitempty"`
}

// UserCount 返回账号总数
func (p *Params) UserCount() int64 { return p.OpenIDEnd - p.OpenIDStart }

// AgentTask Master 通过长轮询下发给 Agent 的单次子任务。
// TaskKey 格式：{reportID}/{caseIndex}/{agentID}，用于结果通道匹配。
// TaskType:
//   - "" / "stress": 执行压测
//   - "control": 执行控制指令（由 ControlParams 描述）
type AgentTask struct {
	TaskType      string        `json:"task_type,omitempty"` // "" / "stress" / "control"
	TaskKey       string        `json:"task_key"`
	ReportID      string        `json:"report_id"`
	CaseIndex     int           `json:"case_index"`
	Params        Params        `json:"params"`
	ControlParams ControlParams `json:"control_params,omitempty"` // TaskType="control" 时使用
	EnableLog     bool          `json:"enable_log"`               // 是否开启日志输出，默认 false
}

// AgentTaskResult Agent 执行完成后通过 HTTP 上报给 Master 的结果。
type AgentTaskResult struct {
	TaskKey  string `json:"task_key"`
	Tracings int    `json:"tracings"`
	Error    string `json:"error,omitempty"`
}

// CaseFileLine 表示 case 文件中解析后的一行，可能是压测指令或控制指令
type CaseFileLine struct {
	// IsControl 为 true 时使用 Control 字段，否则使用 Stress 字段
	IsControl bool
	// Background 是否后台执行（行尾有 &）
	BackgroundRunning bool
	// Stress 压测参数（IsControl=false 时有效）
	Stress Params
	// Control 控制指令参数（IsControl=true 时有效）
	Control ControlParams
}
