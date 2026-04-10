package master

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	robot_case "github.com/atframework/robot-go/case"
	"github.com/atframework/robot-go/report"
	report_impl "github.com/atframework/robot-go/report/impl"
)

// MasterConfig Master 启动配置
type MasterConfig struct {
	ListenAddr   string // HTTP 监听地址，如 ":8080"
	RedisAddr    string
	RedisPwd     string
	ReportDir    string        // HTML 报告输出目录
	ReportExpiry time.Duration // 报告自动过期时长；0 表示永不过期（如 7*24*time.Hour = 7 天）
}

// agentInfo 一个已注册 Agent 的信息
type agentInfo struct {
	ID        string    `json:"id"`
	Addr      string    `json:"addr"`
	GroupID   string    `json:"group_id"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen"`
	sessionID string    // 不导出到 JSON；用于区分同 ID 的不同进程实例
}

// taskStatus 一次分布式任务的状态
type taskStatus struct {
	ReportID       string    `json:"report_id"`
	Status         string    `json:"status"` // pending / running / done / error
	Error          string    `json:"error,omitempty"`
	TargetGroup    string    `json:"target_group,omitempty"`
	TargetAgents   []string  `json:"target_agents,omitempty"`
	DistributeMode string    `json:"distribute_mode,omitempty"`
	SubmittedAt    time.Time `json:"submitted_at"`
}

// Master 分布式压测调度端
type Master struct {
	cfg    MasterConfig
	redis  *redis.Client
	reader *report_impl.RedisReportReader
	gen    *report_impl.EChartsHTMLGenerator

	agents         map[string]*agentInfo
	tasks          map[string]*taskStatus
	agentQueues    map[string]chan *robot_case.AgentTask      // agentID -> 任务入队内存通道
	taskResults    map[string]chan robot_case.AgentTaskResult // taskKey -> 结果通道
	taskCancels    map[string]context.CancelFunc              // reportID -> 取消函数
	agentCancelChs map[string]chan string                     // agentID -> cancel 信号（reportID）
	mu             sync.RWMutex

	server *http.Server
}

// NewMaster 创建 Master 实例并连接 Redis
func NewMaster(cfg MasterConfig) (*Master, error) {
	client, err := report_impl.NewRedisClient(cfg.RedisAddr, cfg.RedisPwd)
	if err != nil {
		return nil, err
	}
	return &Master{
		cfg:            cfg,
		redis:          client,
		reader:         report_impl.NewRedisReportReader(client),
		gen:            report_impl.NewEChartsHTMLGenerator(),
		agents:         make(map[string]*agentInfo),
		tasks:          make(map[string]*taskStatus),
		agentQueues:    make(map[string]chan *robot_case.AgentTask),
		taskResults:    make(map[string]chan robot_case.AgentTaskResult),
		taskCancels:    make(map[string]context.CancelFunc),
		agentCancelChs: make(map[string]chan string),
	}, nil
}

// Start 启动 HTTP API 服务（阻塞）
func (m *Master) Start() error {
	mux := http.NewServeMux()

	// Web Dashboard
	mux.HandleFunc("GET /", m.handleDashboard)

	// API
	mux.HandleFunc("POST /api/agents/register", m.handleAgentRegister)
	mux.HandleFunc("GET /api/agents", m.handleListAgents)
	mux.HandleFunc("POST /api/agents/reboot", m.handleAgentReboot)
	mux.HandleFunc("POST /api/tasks", m.handleSubmitTask)
	mux.HandleFunc("GET /api/tasks/all", m.handleListAllTasks)
	mux.HandleFunc("GET /api/tasks/history", m.handleTaskHistory)
	mux.HandleFunc("GET /api/tasks/{id}", m.handleTaskStatus)
	mux.HandleFunc("GET /api/reports", m.handleListReports)
	mux.HandleFunc("POST /api/reports/{id}/html", m.handleGenerateHTML)
	mux.HandleFunc("DELETE /api/reports/{id}", m.handleDeleteReport)

	mux.HandleFunc("POST /api/tasks/{id}/stop", m.handleStopTask)

	// Agent 长轮询任务 + 结果上报
	mux.HandleFunc("GET /api/agent/tasks/next", m.handleAgentPoll)
	mux.HandleFunc("POST /api/agent/tasks/result", m.handleAgentResult)
	mux.HandleFunc("GET /api/agent/tasks/cancel_watch", m.handleAgentCancelWatch)

	// Report viewer (serves generated HTML files)
	mux.HandleFunc("GET /reports/{id}/view", m.handleViewReport)

	m.server = &http.Server{
		Addr:              m.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if m.cfg.ReportExpiry > 0 {
		go m.startExpiryCleanup()
	}
	go m.startAgentCleanup()
	log.Printf("[Master] Dashboard: http://localhost%s  Redis=%s  ReportDir=%s  Expiry=%s",
		m.cfg.ListenAddr, m.cfg.RedisAddr, m.cfg.ReportDir, m.cfg.ReportExpiry)
	return m.server.ListenAndServe()
}

// Stop 优雅停机
func (m *Master) Stop() error {
	if m.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return m.server.Shutdown(ctx)
	}
	return nil
}

// ---------- API Handlers ----------

func (m *Master) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Addr      string `json:"addr"`
		GroupID   string `json:"group_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	existing, exists := m.agents[req.ID]
	if exists && existing.Status == "online" && existing.sessionID != "" && existing.sessionID != req.SessionID {
		m.mu.Unlock()
		http.Error(w, fmt.Sprintf("agent id %q is already online (session conflict)", req.ID), http.StatusConflict)
		return
	}
	info := &agentInfo{
		ID:        req.ID,
		Addr:      req.Addr,
		GroupID:   req.GroupID,
		Status:    "online",
		LastSeen:  time.Now(),
		sessionID: req.SessionID,
	}
	m.agents[req.ID] = info
	m.mu.Unlock()

	// 写入 Redis
	data, _ := json.Marshal(info)
	m.redis.HSet(context.Background(), "agent:registry", req.ID, string(data))

	log.Printf("[Master] Agent registered: %s (session=%s)", req.ID, req.SessionID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (m *Master) handleListAgents(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	list := make([]*agentInfo, 0, len(m.agents))
	for _, a := range m.agents {
		list = append(list, a)
	}
	m.mu.RUnlock()
	writeJSON(w, http.StatusOK, list)
}

// handleAgentReboot 向目标 Agent（或全部在线 Agent）发送 Reboot 任务（异步执行）。
func (m *Master) handleAgentReboot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentIDs    []string `json:"agent_ids"`
		TargetGroup string   `json:"target_group"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	go func() {
		defer cancel()
		if err := m.rebootAgents(ctx, req.TargetGroup, req.AgentIDs); err != nil {
			log.Printf("[Master] Reboot agents failed: %v", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "reboot_sent"})
}

func (m *Master) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CaseFileContent string   `json:"case_file_content"`
		RepeatedTime    int      `json:"repeated_time"`
		ReportID        string   `json:"report_id"`
		TargetGroup     string   `json:"target_group"`      // 目标组（空 = 全部），组模式
		TargetAgents    []string `json:"target_agents"`     // 指定 Agent ID 列表，Agent 模式（优先级高于 TargetGroup）
		DistributeMode  string   `json:"distribute_mode"`   // "balance"（默认） 或 "copy"
		RebootBefore    bool     `json:"reboot_before_run"` // 执行前先 Reboot 所有目标 Agent
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.CaseFileContent == "" {
		http.Error(w, "case_file_content is required", http.StatusBadRequest)
		return
	}
	if req.RepeatedTime <= 0 {
		req.RepeatedTime = 1
	}
	if req.ReportID == "" {
		var err error
		req.ReportID, err = report_impl.GenerateUniqueReportID(m.redis)
		if err != nil {
			http.Error(w, "generate report ID: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if req.DistributeMode == "" {
		req.DistributeMode = "balance"
	}

	// 校验是否有可用 Agent（online 或 busy 均可接受任务）
	m.mu.RLock()
	agentCount := 0
	for _, a := range m.agents {
		if a.Status != "online" && a.Status != "busy" {
			continue
		}
		if len(req.TargetAgents) > 0 {
			for _, id := range req.TargetAgents {
				if a.ID == id {
					agentCount++
					break
				}
			}
		} else if req.TargetGroup == "" || a.GroupID == req.TargetGroup {
			agentCount++
		}
	}
	m.mu.RUnlock()
	if agentCount == 0 {
		msg := "no agents registered"
		if len(req.TargetAgents) > 0 {
			msg = "no online agents matching the specified agent IDs"
		} else if req.TargetGroup != "" {
			msg = "no online agents in group " + req.TargetGroup
		}
		http.Error(w, msg, http.StatusServiceUnavailable)
		return
	}

	st := &taskStatus{
		ReportID:       req.ReportID,
		Status:         "running",
		TargetGroup:    req.TargetGroup,
		TargetAgents:   req.TargetAgents,
		DistributeMode: req.DistributeMode,
		SubmittedAt:    time.Now(),
	}
	m.mu.Lock()
	m.tasks[req.ReportID] = st
	m.mu.Unlock()

	// 持久化任务历史到 Redis
	historyEntry := map[string]interface{}{
		"report_id":         req.ReportID,
		"case_file_content": req.CaseFileContent,
		"repeated_time":     req.RepeatedTime,
		"target_group":      req.TargetGroup,
		"target_agents":     req.TargetAgents,
		"distribute_mode":   req.DistributeMode,
		"submitted_at":      time.Now().Format(time.RFC3339),
	}
	if data, err := json.Marshal(historyEntry); err == nil {
		m.redis.HSet(context.Background(), "task:history", req.ReportID, string(data))
	}

	// 写 meta
	now := time.Now()
	meta := &report.ReportMeta{
		ReportID:  req.ReportID,
		Title:     "Distributed Stress Test",
		StartTime: now,
		CreatedAt: now,
	}
	if m.cfg.ReportExpiry > 0 {
		exp := now.Add(m.cfg.ReportExpiry)
		meta.ExpiresAt = &exp
	}
	writer := report_impl.NewRedisReportWriter(m.redis, "master")
	_ = writer.WriteMeta(meta)

	// 异步执行分发
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.taskCancels[req.ReportID] = cancel
	m.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			m.mu.Lock()
			delete(m.taskCancels, req.ReportID)
			m.mu.Unlock()
		}()
		if err := m.distributeAndWait(ctx, req.ReportID, req.CaseFileContent, req.RepeatedTime, req.TargetGroup, req.TargetAgents, req.DistributeMode, req.RebootBefore); err != nil {
			log.Printf("[Master] Task %s failed: %v", req.ReportID, err)
			m.mu.Lock()
			st.Status = "error"
			st.Error = err.Error()
			m.mu.Unlock()
			return
		}

		// 压测完成，记录 EndTime 并持久化
		{
			endNow := time.Now()
			endWriter := report_impl.NewRedisReportWriter(m.redis, "master")
			if endMeta, endErr := m.reader.ReadReport(req.ReportID); endErr == nil {
				endMeta.Meta.EndTime = endNow
				_ = endWriter.WriteMeta(&endMeta.Meta)
			}
		}

		// 聚合报告 + 生成 HTML
		if err := m.aggregateAndGenerate(req.ReportID); err != nil {
			log.Printf("[Master] Aggregate %s failed: %v", req.ReportID, err)
			m.mu.Lock()
			st.Status = "error"
			st.Error = err.Error()
			m.mu.Unlock()
			return
		}

		m.mu.Lock()
		st.Status = "done"
		m.mu.Unlock()
		log.Printf("[Master] Task %s completed", req.ReportID)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"report_id": req.ReportID,
		"status":    "running",
	})
}

func (m *Master) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m.mu.RLock()
	st, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (m *Master) handleListReports(w http.ResponseWriter, _ *http.Request) {
	metas, err := m.reader.ListReports()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, metas)
}

func (m *Master) handleGenerateHTML(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := m.aggregateAndGenerate(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "report_id": id})
}

// handleDeleteReport 删除报告：清理 Redis 数据 + 本地文件
func (m *Master) handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if err := m.deleteReport(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "report_id": id})
}

// deleteReport 从 Redis 和磁盘删除报告的所有数据。
func (m *Master) deleteReport(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 删除 meta、tracing、metrics、barrier 的 Redis key
	keysToDelete := []string{fmt.Sprintf("report:meta:%s", id)}
	for _, pattern := range []string{
		fmt.Sprintf("report:tracing:%s:*", id),
		fmt.Sprintf("report:metrics:%s:*", id),
		fmt.Sprintf("task:barrier:%s:*", id),
	} {
		keys, _ := m.scanRedisKeys(ctx, pattern)
		keysToDelete = append(keysToDelete, keys...)
	}
	if len(keysToDelete) > 0 {
		m.redis.Del(ctx, keysToDelete...)
	}
	m.redis.ZRem(ctx, "report:index", id)
	m.redis.HDel(ctx, "task:history", id)

	// 删除本地 HTML 文件（防路径穿越）
	if m.cfg.ReportDir != "" {
		htmlFile := filepath.Join(m.cfg.ReportDir, id+".html")
		absFile, err1 := filepath.Abs(htmlFile)
		absBase, err2 := filepath.Abs(m.cfg.ReportDir)
		if err1 == nil && err2 == nil &&
			strings.HasPrefix(absFile, absBase+string(filepath.Separator)) {
			if err := os.Remove(absFile); err != nil && !os.IsNotExist(err) {
				log.Printf("[Master] Delete report file %s: %v", absFile, err)
			}
		}
	}

	// 从内存任务表移除
	m.mu.Lock()
	delete(m.tasks, id)
	m.mu.Unlock()

	log.Printf("[Master] Report deleted: %s", id)
	return nil
}

// scanRedisKeys 扫描匹配 pattern 的所有 Redis key。
func (m *Master) scanRedisKeys(ctx context.Context, pattern string) ([]string, error) {
	var all []string
	var cursor uint64
	for {
		keys, next, err := m.redis.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return all, err
		}
		all = append(all, keys...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return all, nil
}

// startExpiryCleanup 后台定期清理已过期的报告（每 10 分钟检查一次）。
func (m *Master) startExpiryCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		metas, err := m.reader.ListReports()
		if err != nil {
			log.Printf("[Master] Expiry check failed: %v", err)
			continue
		}
		now := time.Now()
		for _, meta := range metas {
			if meta.ExpiresAt != nil && !meta.ExpiresAt.IsZero() && now.After(*meta.ExpiresAt) {
				log.Printf("[Master] Report %s expired (%s), deleting", meta.ReportID, meta.ExpiresAt.Format(time.RFC3339))
				if err := m.deleteReport(meta.ReportID); err != nil {
					log.Printf("[Master] Delete expired report %s: %v", meta.ReportID, err)
				}
			}
		}
	}
}

// startAgentCleanup 后台每 10s 检查：offline 超过 5 分钟的 Agent 从注册表删除。
// 下线检测由 handleAgentPoll 的连接断开事件实时触发，此处仅做延迟清理。
func (m *Master) startAgentCleanup() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for id, a := range m.agents {
			if (a.Status == "offline" || a.Status == "busy") && now.Sub(a.LastSeen) > 5*time.Minute {
				log.Printf("[Master] Agent %s removed (offline for %.0fs)", id, now.Sub(a.LastSeen).Seconds())
				delete(m.agents, id)
				delete(m.agentQueues, id)
				delete(m.agentCancelChs, id)
				m.redis.HDel(context.Background(), "agent:registry", id)
			}
		}
		m.mu.Unlock()
	}
}

// ---------- Agent Long-Poll ----------

// handleAgentPoll Agent 长轮询接口：阻塞最多 30s 等待任务下发；无任务则返回 204。
// 每次长轮询连接建立即视为 Agent 在线；连接断开（r.Context 取消）时立即标记 offline。
func (m *Master) handleAgentPoll(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}

	// 连接建立：标记在线并更新 LastSeen
	m.mu.Lock()
	if info, ok := m.agents[agentID]; ok {
		info.LastSeen = time.Now()
		info.Status = "online"
	}
	queue := m.getOrCreateAgentQueueLocked(agentID)
	m.mu.Unlock()

	taskSent := false
	clientDisconnected := false
	// 连接断开时处理 Agent 状态：
	//   - 已下发任务 → busy
	//   - 客户端真正断开（非 poll 超时）→ offline
	//   - poll 正常超时无任务（返回 204）→ 保持 online，不打 offline 日志
	defer func() {
		m.mu.Lock()
		if info, ok := m.agents[agentID]; ok {
			if taskSent {
				info.Status = "busy"
			} else if clientDisconnected && info.Status == "online" {
				info.Status = "offline"
				log.Printf("[Master] Agent %s offline (connection closed)", agentID)
			}
			info.LastSeen = time.Now()
		}
		m.mu.Unlock()
	}()

	parentCtx := r.Context()
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	select {
	case task := <-queue:
		taskSent = true
		writeJSON(w, http.StatusOK, task)
	case <-ctx.Done():
		// 若是 parentCtx 被取消（客户端断开），才标记下线；若只是 poll 30s 超时，保持在线
		if parentCtx.Err() != nil {
			clientDisconnected = true
		}
		w.WriteHeader(http.StatusNoContent) // 204: 暂无任务，Agent 应立即重试
	}
}

// handleAgentResult 接收 Agent 执行结果，唤醒等待中的 enqueueAgentTask。
func (m *Master) handleAgentResult(w http.ResponseWriter, r *http.Request) {
	var res robot_case.AgentTaskResult
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.RLock()
	ch, ok := m.taskResults[res.TaskKey]
	m.mu.RUnlock()

	if !ok {
		log.Printf("[Master] Received result for unknown/expired task %s", res.TaskKey)
	} else {
		select {
		case ch <- res:
		default:
			log.Printf("[Master] Result channel full for task %s", res.TaskKey)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleStopTask 取消正在运行的任务，与 Reboot 相同流程：主动向 Agent 推送取消信号。
func (m *Master) handleStopTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m.mu.RLock()
	cancelFn, hasCancelFn := m.taskCancels[id]
	st, hasSt := m.tasks[id]
	m.mu.RUnlock()

	if !hasSt {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if st.Status != "running" {
		http.Error(w, "task is not running", http.StatusBadRequest)
		return
	}
	// 1. 停止 distributor goroutine（不再向 Agent 分发新 case）
	if hasCancelFn {
		cancelFn()
	}
	m.mu.Lock()
	st.Status = "stopped"
	st.Error = "stopped by user"
	m.mu.Unlock()

	// 2. 主动向各目标 Agent 推送取消信号（与 Reboot 相同的推送流程）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go func() {
		defer cancel()
		m.sendCancelToAgents(ctx, id, st.TargetGroup, st.TargetAgents)
	}()

	log.Printf("[Master] Task %s stopped by user", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "report_id": id})
}

// handleAgentCancelWatch Agent 长轮询接口：阻塞最多 30s 等待 Master 主动推送取消信号。
// 与 handleAgentPoll 类似，但专用于取消通知，避免 Agent 轮询 cancel 状态的开销。
func (m *Master) handleAgentCancelWatch(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	ch := m.getOrCreateAgentCancelChLocked(agentID)
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	select {
	case reportID := <-ch:
		writeJSON(w, http.StatusOK, map[string]string{"cancel_report_id": reportID})
	case <-ctx.Done():
		w.WriteHeader(http.StatusNoContent) // 204：无取消信号，Agent 应立即重试
	}
}

// sendCancelToAgents 向目标 Agent 推送取消信号（reportID），与 rebootAgents 推送逻辑对称。
func (m *Master) sendCancelToAgents(ctx context.Context, reportID, targetGroup string, targetAgents []string) {
	_ = ctx // 保留 ctx 参数，便于未来扩展（如发送超时控制）
	agentSet := make(map[string]struct{}, len(targetAgents))
	for _, id := range targetAgents {
		agentSet[id] = struct{}{}
	}

	m.mu.RLock()
	agents := make([]*agentInfo, 0, len(m.agents))
	for _, a := range m.agents {
		if len(agentSet) > 0 {
			if _, ok := agentSet[a.ID]; ok {
				agents = append(agents, a)
			}
		} else if targetGroup == "" || a.GroupID == targetGroup {
			agents = append(agents, a)
		}
	}
	m.mu.RUnlock()

	if len(agents) == 0 {
		return
	}

	m.mu.Lock()
	for _, a := range agents {
		ch := m.getOrCreateAgentCancelChLocked(a.ID)
		select {
		case ch <- reportID:
		default:
			log.Printf("[Master] Cancel channel full for agent %s, signal dropped", a.ID)
		}
	}
	m.mu.Unlock()
	log.Printf("[Master] Cancel signal for report %s sent to %d agent(s)", reportID, len(agents))
}

// getOrCreateAgentCancelChLocked 获取或创建 agent 的取消信号通道（调用时必须持有写锁）。
func (m *Master) getOrCreateAgentCancelChLocked(agentID string) chan string {
	if ch, ok := m.agentCancelChs[agentID]; ok {
		return ch
	}
	ch := make(chan string, 8)
	m.agentCancelChs[agentID] = ch
	return ch
}

// ---------- Dashboard & Report Viewer ----------

func (m *Master) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

func (m *Master) handleListAllTasks(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	list := make([]*taskStatus, 0, len(m.tasks))
	for _, t := range m.tasks {
		list = append(list, t)
	}
	m.mu.RUnlock()
	sort.Slice(list, func(i, j int) bool {
		return list[i].SubmittedAt.Before(list[j].SubmittedAt)
	})
	writeJSON(w, http.StatusOK, list)
}

func (m *Master) handleTaskHistory(w http.ResponseWriter, _ *http.Request) {
	result, err := m.redis.HGetAll(context.Background(), "task:history").Result()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var list []json.RawMessage
	for _, v := range result {
		list = append(list, json.RawMessage(v))
	}
	sort.Slice(list, func(i, j int) bool {
		var a, b struct {
			SubmittedAt string `json:"submitted_at"`
		}
		json.Unmarshal(list[i], &a)
		json.Unmarshal(list[j], &b)
		return a.SubmittedAt < b.SubmittedAt
	})
	writeJSON(w, http.StatusOK, list)
}

func (m *Master) handleViewReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	htmlPath := filepath.Join(m.cfg.ReportDir, id+".html")
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		// 尝试实时生成
		if genErr := m.aggregateAndGenerate(id); genErr != nil {
			http.Error(w, "report not found and generate failed: "+genErr.Error(), http.StatusNotFound)
			return
		}
		data, err = os.ReadFile(htmlPath)
		if err != nil {
			http.Error(w, "read generated report: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
