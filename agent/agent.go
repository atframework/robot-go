package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	robot_case "github.com/atframework/robot-go/case"
	user_data "github.com/atframework/robot-go/data"
	"github.com/atframework/robot-go/report"
	report_impl "github.com/atframework/robot-go/report/impl"
)

// ErrAgentIDConflict 同一 Agent ID 已被其他在线实例占用。
var ErrAgentIDConflict = errors.New("agent ID conflict: another instance with this ID is already online")

// AgentConfig Agent 启动配置
type AgentConfig struct {
	MasterAddr string // Master HTTP 地址
	RedisAddr  string
	RedisPwd   string
	AgentID    string // 唯一标识（默认 hostname+pid）
	GroupID    string // 组 ID（可选，Master 按组分发任务）
	SessionID  string // 进程级会话 ID，由 NewAgent 自动生成；用于 Master 检测 ID 冲突
}

// Agent 分布式压测执行端（主动向 Master 拉取任务）
type Agent struct {
	cfg           AgentConfig
	writer        *report_impl.RedisReportWriter
	client        *http.Client
	onlineMetrics *report_impl.MemoryMetricsCollector // 进程级 online_users，跨 task 持续采集
}

// NewAgent 创建 Agent 实例并连接 Redis
func NewAgent(cfg AgentConfig) (*Agent, error) {
	if cfg.AgentID == "" {
		host, _ := os.Hostname()
		cfg.AgentID = fmt.Sprintf("agent-%s-%d", host, os.Getpid())
	}
	if cfg.SessionID == "" {
		// 用 hostname+pid+启动时间纳秒 生成进程唯一会话 ID
		host, _ := os.Hostname()
		cfg.SessionID = fmt.Sprintf("%s-%d-%d", host, os.Getpid(), time.Now().UnixNano())
	}

	redisClient, err := report_impl.NewRedisClient(cfg.RedisAddr, cfg.RedisPwd)
	if err != nil {
		return nil, err
	}

	om := report_impl.NewMemoryMetricsCollector()
	om.Register("online_users", func() float64 {
		return float64(user_data.OnlineUserCount())
	})
	om.StartAutoCollect(time.Second)

	return &Agent{
		cfg:           cfg,
		writer:        report_impl.NewRedisReportWriter(redisClient, cfg.AgentID),
		client:        &http.Client{Timeout: 40 * time.Second},
		onlineMetrics: om,
	}, nil
}

// Start 注册到 Master，然后进入长轮询循环（阻塞）。
// 每次 poll 连接建立即作为在线心跳，无需独立 heartbeatLoop。
func (a *Agent) Start() error {
	log.Printf("[Agent] %s started, Master=%s, Redis=%s", a.cfg.AgentID, a.cfg.MasterAddr, a.cfg.RedisAddr)
	a.pollLoop()
	return nil
}

// pollLoop 持续向 Master 发起长轮询（阻塞，永不返回）。
// 每次发起 poll 前重新注册，作为心跳与连接建立信号。
func (a *Agent) pollLoop() {
	for {
		// 每轮 poll 前确保已注册，同时通知 Master 连接即将建立
		if err := a.registerToMaster(); err != nil {
			if errors.Is(err, ErrAgentIDConflict) {
				log.Fatalf("[Agent] %v (agent-id=%s) — 请更换 agent-id 或等待旧实例下线", err, a.cfg.AgentID)
			}
			log.Printf("[Agent] register failed: %v, retry in 3s", err)
			time.Sleep(3 * time.Second)
			continue
		}
		task, err := a.pollTask()
		if err != nil {
			log.Printf("[Agent] poll error: %v, retry in 3s", err)
			time.Sleep(3 * time.Second)
			continue
		}
		if task == nil {
			// 204 No Content：master 无任务，poll 本身已包含 30s 等待，立即重试
			continue
		}
		a.executeTask(task)
	}
}

// pollTask 向 Master 拉取一个任务；返回 nil,nil 表示 204 当前无任务
func (a *Agent) pollTask() (*robot_case.AgentTask, error) {
	u, _ := url.Parse(a.masterURL("/api/agent/tasks/next"))
	q := u.Query()
	q.Set("agent_id", a.cfg.AgentID)
	u.RawQuery = q.Encode()

	resp, err := a.client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from master poll", resp.StatusCode)
	}
	var task robot_case.AgentTask
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, fmt.Errorf("decode task: %w", err)
	}
	return &task, nil
}

// executeTask 执行一个任务。若 TaskType 为 "reboot" 则重置内部状态；
// 否则执行压测任务，将数据写入 Redis，并上报结果给 Master。
func (a *Agent) executeTask(task *robot_case.AgentTask) {
	if task.TaskType == "reboot" {
		a.performReboot(task)
		return
	}
	log.Printf("[Agent] Executing task: key=%s report=%s case=%d name=%s IDs=[%d,%d) QPS=%.1f",
		task.TaskKey, task.ReportID, task.CaseIndex, task.Params.CaseName,
		task.Params.OpenIDStart, task.Params.OpenIDEnd, task.Params.TargetQPS)

	tracer := report_impl.NewMemoryTracer()
	pressure := report_impl.NewMemoryPressureController()

	// 丢弃 task 开始前积累的 online_users 历史数据（agent 启动到 task 开始之间的数据不属于本次报告）
	_ = a.onlineMetrics.Flush()

	// 立即采一次在线用户初始值（持续采集已在 Agent 初始化时启动）
	a.onlineMetrics.Collect()

	// 创建 cancel context，用于取消机制
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 定期 flush 部分数据到 Redis 和检查 cancel
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 检查任务是否被取消
				if a.checkCancelled(task.ReportID) {
					cancel()
					return
				}
				// flush 部分打点数据
				a.flushPartialData(task.ReportID, task.Params.CaseName, tracer, pressure)
			}
		}
	}()

	errMsg := robot_case.RunCaseStressWithContext(ctx, task.Params, tracer, pressure)

	// 最终再采集一次在线用户（持续采集不停止，只 flush 增量数据）
	a.onlineMetrics.Collect()

	// 停止 flush goroutine
	cancel()
	<-flushDone

	// 最终 flush 剩余数据
	tracings := tracer.Flush()
	var metricsData []*report.MetricsSeries

	// 使用 FlushSnapshots 只取本次任务新增的快照，避免重复写入
	snapshots := pressure.FlushSnapshots()
	if len(snapshots) > 0 {
		var pressurePts, throttlePts, actualQPSPts []report.MetricsPoint
		for _, s := range snapshots {
			pressurePts = append(pressurePts, report.MetricsPoint{Timestamp: s.Timestamp, Value: float64(s.Level)})
			throttlePts = append(throttlePts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ThrottleRatio})
			actualQPSPts = append(actualQPSPts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ActualQPS})
		}
		metricsData = append(metricsData,
			&report.MetricsSeries{Name: "pressure_level", Labels: map[string]string{"agent": a.cfg.AgentID, "case": task.Params.CaseName}, Points: pressurePts},
			&report.MetricsSeries{Name: "throttle_ratio", Labels: map[string]string{"agent": a.cfg.AgentID, "case": task.Params.CaseName}, Points: throttlePts},
			&report.MetricsSeries{Name: "actual_qps", Labels: map[string]string{"agent": a.cfg.AgentID, "case": task.Params.CaseName}, Points: actualQPSPts},
		)
	}

	// online_users：Flush 取本轮新增采样点（agent 级持续采集，每 task 写入增量）
	onlineMetrics := a.onlineMetrics.Flush()
	for _, s := range onlineMetrics {
		if s.Labels == nil {
			s.Labels = make(map[string]string)
		}
		s.Labels["agent"] = a.cfg.AgentID
	}
	metricsData = append(metricsData, onlineMetrics...)

	if err := a.writer.WriteTracings(task.ReportID, tracings); err != nil {
		log.Printf("[Agent] write tracings error: %v", err)
	}
	if err := a.writer.WriteMetrics(task.ReportID, metricsData); err != nil {
		log.Printf("[Agent] write metrics error: %v", err)
	}
	if err := a.writer.BarrierACK(task.ReportID, task.CaseIndex); err != nil {
		log.Printf("[Agent] barrier ack error: %v", err)
	}

	log.Printf("[Agent] Task done: key=%s tracings=%d", task.TaskKey, len(tracings))

	if err := a.postResult(robot_case.AgentTaskResult{
		TaskKey:  task.TaskKey,
		Tracings: len(tracings),
		Error:    errMsg,
	}); err != nil {
		log.Printf("[Agent] post result error: %v", err)
	}
}

// performReboot 进程级重启：登出所有用户、上报完成后重新拉起自身进程。
// -agent-id 会被注入到启动参数，保证新进程使用相同的 AgentID。
func (a *Agent) performReboot(task *robot_case.AgentTask) {
	log.Printf("[Agent] Process reboot requested for %s", a.cfg.AgentID)
	user_data.LogoutAllUsers()

	// 先上报结果，让 Master 解除对本任务的阻塞
	if task.TaskKey != "" {
		if err := a.postResult(robot_case.AgentTaskResult{
			TaskKey: task.TaskKey,
		}); err != nil {
			log.Printf("[Agent] post reboot result error: %v", err)
		}
	}

	// 短暂等待，确保 HTTP 响应已完全发送
	time.Sleep(300 * time.Millisecond)

	if err := a.execSelf(); err != nil {
		log.Printf("[Agent] Process restart failed: %v", err)
	}
}

// execSelf 重新启动当前可执行文件（带相同启动参数），然后退出当前进程。
// 会自动注入 -agent-id 以确保新进程 AgentID 不变。
func (a *Agent) execSelf() error {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	args := injectFlagArg(os.Args[1:], "agent-id", a.cfg.AgentID)
	log.Printf("[Agent] Restarting: %s %v", exe, args)
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start new process: %w", err)
	}
	log.Printf("[Agent] New process started (PID %d), exiting current", cmd.Process.Pid)
	os.Exit(0)
	return nil // unreachable
}

// injectFlagArg 确保 args 中包含 -<name> <value>；若已存在则不修改。
func injectFlagArg(args []string, name, value string) []string {
	for _, a := range args {
		if a == "-"+name || a == "--"+name ||
			strings.HasPrefix(a, "-"+name+"=") ||
			strings.HasPrefix(a, "--"+name+"=") {
			return args
		}
	}
	return append(append([]string{}, args...), "-"+name, value)
}

// flushPartialData 将内存中的部分打点数据 flush 到 Redis（增量写入）。
func (a *Agent) flushPartialData(reportID string, caseName string, tracer *report_impl.MemoryTracer, pressure *report_impl.MemoryPressureController) {
	// Flush 会清空内存缓冲，RPush 到 Redis 是增量累加
	tracings := tracer.Flush()
	if len(tracings) > 0 {
		if err := a.writer.WriteTracings(reportID, tracings); err != nil {
			log.Printf("[Agent] partial flush tracings error: %v", err)
		} else {
			log.Printf("[Agent] Partial flush: %d tracings written to Redis", len(tracings))
		}
	}

	// 使用 FlushSnapshots 只取增量快照，避免 partial flush 重复写入已提交数据
	snapshots := pressure.FlushSnapshots()
	if len(snapshots) > 0 {
		var metricsData []*report.MetricsSeries
		var pressurePts, throttlePts, actualQPSPts []report.MetricsPoint
		for _, s := range snapshots {
			pressurePts = append(pressurePts, report.MetricsPoint{Timestamp: s.Timestamp, Value: float64(s.Level)})
			throttlePts = append(throttlePts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ThrottleRatio})
			actualQPSPts = append(actualQPSPts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ActualQPS})
		}
		metricsData = append(metricsData,
			&report.MetricsSeries{Name: "pressure_level", Labels: map[string]string{"agent": a.cfg.AgentID, "case": caseName}, Points: pressurePts},
			&report.MetricsSeries{Name: "throttle_ratio", Labels: map[string]string{"agent": a.cfg.AgentID, "case": caseName}, Points: throttlePts},
			&report.MetricsSeries{Name: "actual_qps", Labels: map[string]string{"agent": a.cfg.AgentID, "case": caseName}, Points: actualQPSPts},
		)
		if err := a.writer.WriteMetrics(reportID, metricsData); err != nil {
			log.Printf("[Agent] partial flush metrics error: %v", err)
		}
	}
}

// checkCancelled 向 Master 查询任务是否已被取消。
func (a *Agent) checkCancelled(reportID string) bool {
	u, _ := url.Parse(a.masterURL("/api/agent/tasks/cancel"))
	q := u.Query()
	q.Set("report_id", reportID)
	u.RawQuery = q.Encode()

	resp, err := a.client.Get(u.String())
	if err != nil {
		return false // 网络错误不当作取消
	}
	defer resp.Body.Close()

	var result struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}
	return result.Cancelled
}

// postResult 向 Master 上报任务结果
func (a *Agent) postResult(result robot_case.AgentTaskResult) error {
	body, _ := json.Marshal(result)
	resp, err := a.client.Post(a.masterURL("/api/agent/tasks/result"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master result endpoint returned %d", resp.StatusCode)
	}
	return nil
}

// masterURL 返回去掉末尾斜杠的 MasterAddr，避免拼接出双斜杠路径。
func (a *Agent) masterURL(path string) string {
	return strings.TrimRight(a.cfg.MasterAddr, "/") + path
}

// registerToMaster 向 Master 注册本 Agent（上报 ID 和组信息）。
// pollLoop 每次建立新 poll 连接前都会调用，兼作在线心跳。
func (a *Agent) registerToMaster() error {
	if a.cfg.MasterAddr == "" {
		return nil
	}
	payload := map[string]string{
		"id":         a.cfg.AgentID,
		"session_id": a.cfg.SessionID,
	}
	if a.cfg.GroupID != "" {
		payload["group_id"] = a.cfg.GroupID
	}
	body, _ := json.Marshal(payload)
	resp, err := a.client.Post(a.masterURL("/api/agents/register"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: %s", ErrAgentIDConflict, strings.TrimSpace(string(msg)))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master returned %d", resp.StatusCode)
	}
	return nil
}
