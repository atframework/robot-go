package master

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	robot_case "github.com/atframework/robot-go/case"
	"github.com/atframework/robot-go/report"
	report_impl "github.com/atframework/robot-go/report/impl"
)

// distributeAndWait 解析 case 文件、按行分发给 Agent 并等待全部完成。
// targetGroup 为空时分发给所有在线 Agent；distributeMode 为 "copy" 或 "balance"。
// rebootBefore=true 时将在分发正式任务前先向所有目标 Agent 发送 Reboot 命令。
func (m *Master) distributeAndWait(ctx context.Context, reportID, caseFileContent string, repeatedTime int, targetGroup string, targetAgents []string, distributeMode string, rebootBefore bool) error {
	// 先 Reboot 目标 Agent（如果需要）
	if rebootBefore {
		log.Printf("[Master] Rebooting agents before task %s", reportID)
		if err := m.rebootAgents(ctx, targetGroup, targetAgents); err != nil {
			log.Printf("[Master] Reboot agents warning: %v", err)
		}
	}

	// 解析 case 文件
	err, lines := parseCaseContent(caseFileContent)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("no case lines found")
	}

	for round := 0; round < repeatedTime; round++ {
		for i, params := range lines {
			// 检查是否已取消
			select {
			case <-ctx.Done():
				return fmt.Errorf("task cancelled")
			default:
			}

			caseIndex := round*len(lines) + i
			if err := m.distributeSingleCase(ctx, reportID, caseIndex, params, targetGroup, targetAgents, distributeMode); err != nil {
				if params.ErrorBreak {
					return fmt.Errorf("case[%d] %s: %w", caseIndex, params.CaseName, err)
				}
				log.Printf("[Master] Case[%d] %s round=%d completed with errors (ErrorBreak=false, continuing): %v", caseIndex, params.CaseName, round, err)
			} else {
				log.Printf("[Master] Case[%d] %s round=%d completed", caseIndex, params.CaseName, round)
			}
		}
	}
	return nil
}

// distributeSingleCase 将一个 case 分发给各 agent 并等待完成。
// distributeMode="copy": 每个 Agent 跑全量 OpenID 与 QPS（完全复制）
// distributeMode="balance": 拆分 ID 范围与 QPS（负载均衡，默认）
func (m *Master) distributeSingleCase(ctx context.Context, reportID string, caseIndex int, params robot_case.Params, targetGroup string, targetAgents []string, distributeMode string) error {
	// 构建 targetAgents 的快速查找集合
	agentSet := make(map[string]struct{}, len(targetAgents))
	for _, id := range targetAgents {
		agentSet[id] = struct{}{}
	}

	m.mu.RLock()
	agents := make([]*agentInfo, 0, len(m.agents))
	for _, a := range m.agents {
		if a.Status != "online" && a.Status != "busy" {
			continue
		}
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
		if len(agentSet) > 0 {
			return fmt.Errorf("no online agents matching the specified agent IDs")
		}
		if targetGroup != "" {
			return fmt.Errorf("no online agents in group %q", targetGroup)
		}
		return fmt.Errorf("no online agents")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(agents))

	if distributeMode == "copy" {
		// 完全复制模式：每个 Agent 拿到完整的参数
		for _, agent := range agents {
			taskKey := fmt.Sprintf("%s/%d/%s", reportID, caseIndex, agent.ID)
			task := &robot_case.AgentTask{
				TaskKey:   taskKey,
				ReportID:  reportID,
				CaseIndex: caseIndex,
				Params:    params,
				EnableLog: false,
			}
			wg.Add(1)
			go func(a *agentInfo, t *robot_case.AgentTask) {
				defer wg.Done()
				if err := m.enqueueAgentTask(ctx, a, t); err != nil {
					errCh <- fmt.Errorf("agent %s: %w", a.ID, err)
				}
			}(agent, task)
		}
	} else {
		// 负载均衡模式：拆分 ID 范围与 QPS
		totalIDs := params.OpenIDEnd - params.OpenIDStart
		perAgent := (totalIDs + int64(len(agents)) - 1) / int64(len(agents))

		for i, agent := range agents {
			start := params.OpenIDStart + int64(i)*perAgent
			end := start + perAgent
			if end > params.OpenIDEnd {
				end = params.OpenIDEnd
			}
			if start >= end {
				continue
			}

			split := params
			split.OpenIDStart = start
			split.OpenIDEnd = end

			// 按比例拆分 QPS
			if params.TargetQPS > 0 {
				split.TargetQPS = params.TargetQPS / float64(len(agents))
			}

			taskKey := fmt.Sprintf("%s/%d/%s", reportID, caseIndex, agent.ID)
			task := &robot_case.AgentTask{
				TaskKey:   taskKey,
				ReportID:  reportID,
				CaseIndex: caseIndex,
				Params:    split,
				EnableLog: false,
			}

			wg.Add(1)
			go func(a *agentInfo, t *robot_case.AgentTask) {
				defer wg.Done()
				if err := m.enqueueAgentTask(ctx, a, t); err != nil {
					errCh <- fmt.Errorf("agent %s: %w", a.ID, err)
				}
			}(agent, task)
		}
	}

	wg.Wait()
	close(errCh)

	// 收集错误
	var errs []string
	for err := range errCh {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// rebootAgents 向目标 Agent 发送 Reboot 任务并等待所有 Agent 响应。
// 若指定了 targetAgents，则只 Reboot 这些 Agent；否则按 targetGroup 过滤（空 = 全部在线 Agent）。
func (m *Master) rebootAgents(ctx context.Context, targetGroup string, targetAgents []string) error {
	agentSet := make(map[string]struct{}, len(targetAgents))
	for _, id := range targetAgents {
		agentSet[id] = struct{}{}
	}

	m.mu.RLock()
	agents := make([]*agentInfo, 0, len(m.agents))
	for _, a := range m.agents {
		if a.Status != "online" && a.Status != "busy" {
			continue
		}
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
		return nil
	}

	stamp := time.Now().Format("20060102-150405")
	var wg sync.WaitGroup
	errCh := make(chan error, len(agents))
	for _, agent := range agents {
		taskKey := fmt.Sprintf("reboot/%s/%s", stamp, agent.ID)
		task := &robot_case.AgentTask{
			TaskType:  "reboot",
			TaskKey:   taskKey,
			EnableLog: false,
		}
		wg.Add(1)
		go func(a *agentInfo, t *robot_case.AgentTask) {
			defer wg.Done()
			if err := m.enqueueAgentTask(ctx, a, t); err != nil {
				errCh <- fmt.Errorf("reboot agent %s: %w", a.ID, err)
			}
		}(agent, task)
	}
	wg.Wait()
	close(errCh)

	var errs []string
	for err := range errCh {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	log.Printf("[Master] Rebooted %d agent(s)", len(agents))
	return nil
}

// enqueueAgentTask 将任务放入 agent 的内存队列，并阻塞到 agent 执行完成后返回。
func (m *Master) enqueueAgentTask(ctx context.Context, ag *agentInfo, task *robot_case.AgentTask) error {
	resultCh := make(chan robot_case.AgentTaskResult, 1)
	m.mu.Lock()
	m.taskResults[task.TaskKey] = resultCh
	queue := m.getOrCreateAgentQueueLocked(ag.ID)
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.taskResults, task.TaskKey)
		m.mu.Unlock()
	}()

	// 入队，等待 agent 开始轮询（最多 35s）
	select {
	case queue <- task:
	case <-ctx.Done():
		return fmt.Errorf("task cancelled")
	case <-time.After(35 * time.Second):
		return fmt.Errorf("agent %s 没有在轮询，入队超时", ag.ID)
	}

	// 等待执行结果（最多 35 分钟）
	select {
	case res := <-resultCh:
		if res.Error != "" {
			return fmt.Errorf("%s", res.Error)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("task cancelled")
	case <-time.After(35 * time.Minute):
		return fmt.Errorf("agent %s 任务 %s 执行超时(35min)", ag.ID, task.TaskKey)
	}
}

// getOrCreateAgentQueueLocked 获取或创建 agent 的任务队列（调用时必须持有写锁）。
func (m *Master) getOrCreateAgentQueueLocked(agentID string) chan *robot_case.AgentTask {
	if q, ok := m.agentQueues[agentID]; ok {
		return q
	}
	q := make(chan *robot_case.AgentTask, 8)
	m.agentQueues[agentID] = q
	return q
}

// aggregateAndGenerate 从 Redis 汇总各 Agent 的数据并生成 HTML 报告。
func (m *Master) aggregateAndGenerate(reportID string) error {
	data, err := m.reader.ReadReport(reportID)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}

	// 追加打点清洗后的指标
	cleaned := report.CleanTracingsToMetrics(data.Tracings)
	data.Metrics = append(data.Metrics, cleaned...)

	// 从 Redis key 中提取参与本次报告的 Agent ID 列表
	ctx := context.Background()
	tracingKeys, _ := m.scanRedisKeys(ctx, fmt.Sprintf("report:tracing:%s:*", reportID))
	prefix := fmt.Sprintf("report:tracing:%s:", reportID)
	agentIDSet := make(map[string]struct{})
	for _, key := range tracingKeys {
		agentID := strings.TrimPrefix(key, prefix)
		if agentID != "" {
			agentIDSet[agentID] = struct{}{}
		}
	}
	if len(agentIDSet) > 0 {
		agentIDs := make([]string, 0, len(agentIDSet))
		for id := range agentIDSet {
			agentIDs = append(agentIDs, id)
		}
		sort.Strings(agentIDs)
		data.Meta.AgentIDs = agentIDs
	}

	data.Meta.RawDataSize = 0
	// 计算原始数据大小（近似 Redis 占用：tracings + metrics 序列化大小）
	if tb, err := json.Marshal(data.Tracings); err == nil {
		data.Meta.RawDataSize += int64(len(tb))
	}
	if mb, err := json.Marshal(data.Metrics); err == nil {
		data.Meta.RawDataSize += int64(len(mb))
	}

	htmlPath := filepath.Join(m.cfg.ReportDir, reportID+".html")
	// 使用上一次生成的 HTML 文件大小作为初始 ReportSize 估值
	if fi, statErr := os.Stat(htmlPath); statErr == nil {
		data.Meta.ReportSize = fi.Size()
	}

	redisWriter := report_impl.NewRedisReportWriter(m.redis, "master")
	_ = redisWriter.WriteMeta(&data.Meta)

	_ = os.MkdirAll(m.cfg.ReportDir, 0750)

	// 生成 HTML（数据内嵌，不再写单独 JSON 文件）
	if err := m.gen.GenerateToFile(data, htmlPath); err != nil {
		return fmt.Errorf("generate html: %w", err)
	}

	// 生成完毕后更新实际报告大小，并回写 meta
	if fi, statErr := os.Stat(htmlPath); statErr == nil {
		data.Meta.ReportSize = fi.Size()
		_ = redisWriter.WriteMeta(&data.Meta)
	}

	log.Printf("[Master] Report generated: %s", htmlPath)
	return nil
}

// parseCaseContent 解析 case 文件内容，返回 (是否 stress, 各行参数)。
func parseCaseContent(content string) (error, []robot_case.Params) {
	scanner := bufio.NewScanner(strings.NewReader(content))

	var lines []robot_case.Params
	for scanner.Scan() {
		raw := scanner.Text()
		// 去注释
		if idx := strings.Index(raw, "#"); idx >= 0 {
			raw = raw[:idx]
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		args := strings.Fields(line)
		if strings.ToLower(args[len(args)-1]) == "&" {
			args = args[:len(args)-1]
		}
		if len(args) == 0 {
			continue
		}

		params, err := robot_case.ParseStressLine(args)
		if err != nil {
			return err, nil
		}
		params.CaseIndex = len(lines)
		lines = append(lines, params)
	}
	return nil, lines
}

// RegisterAgentFromRedis 从 Redis 恢复已注册的 Agent
func (m *Master) RegisterAgentFromRedis() {
	ctx := context.Background()
	result, err := m.redis.HGetAll(ctx, "agent:registry").Result()
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, data := range result {
		var info agentInfo
		if err := json.Unmarshal([]byte(data), &info); err != nil {
			continue
		}
		info.ID = id
		m.agents[id] = &info
	}
}

// ---------- 公开 parseStressLine (从 case 包导出) ----------
// 为了让 master 能调用 case.parseStressLine，需要在 case 包中导出该函数。
// 参见对 case/action.go 的修改：新增 ParseStressLine 公开函数。

// ExportReportData 返回聚合后的数据（供 CLI 使用）
func (m *Master) ExportReportData(reportID string) (*report.ReportData, error) {
	return m.reader.ReadReport(reportID)
}
