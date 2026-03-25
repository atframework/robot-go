package atsf4g_go_robot_case

// StressParams 压测运行参数，由 Master 分发给 Agent 时携带
type StressParams struct {
	// 用例名称
	CaseName string `json:"case_name"`
	// 错误是否打断：true 时遇到第一个错误即停止整个压测
	ErrorBreak bool `json:"error_break"`

	// 账号 ID 范围（半开区间）: [OpenIDStart, OpenIDEnd)
	OpenIDPrefix string `json:"openid_prefix"`
	OpenIDStart  int64  `json:"openid_start"`
	OpenIDEnd    int64  `json:"openid_end"`

	// QPS 控制：目标 QPS，0 表示不限速（兼容旧模式）
	TargetQPS float64 `json:"target_qps"`
	// 最大并发数（对应原 batchCount）
	BatchCount int64 `json:"batch_count"`

	// 运行持续时间（秒），0 表示每个账号只跑一次
	RunTime int64 `json:"run_time"`

	// 额外透传参数
	ExtraArgs []string `json:"extra_args,omitempty"`
}

// UserCount 返回账号总数
func (p *StressParams) UserCount() int64 { return p.OpenIDEnd - p.OpenIDStart }

// AgentTask Master 通过长轮询下发给 Agent 的单次子任务。
// TaskKey 格式：{reportID}/{caseIndex}/{agentID}，用于结果通道匹配。
// TaskType 为空或 "stress" 时执行压测；"reboot" 时 Agent 重置内部状态。
type AgentTask struct {
	TaskType  string       `json:"task_type,omitempty"` // "" / "stress" / "reboot"
	TaskKey   string       `json:"task_key"`
	ReportID  string       `json:"report_id"`
	CaseIndex int          `json:"case_index"`
	Params    StressParams `json:"params"`
	EnableLog bool         `json:"enable_log"` // 是否开启日志输出，默认 false
}

// AgentTaskResult Agent 执行完成后通过 HTTP 上报给 Master 的结果。
type AgentTaskResult struct {
	TaskKey  string `json:"task_key"`
	Tracings int    `json:"tracings"`
	Error    string `json:"error,omitempty"`
}
