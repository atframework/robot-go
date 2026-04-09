package atsf4g_go_robot_user

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	robot_case "github.com/atframework/robot-go/case"
	user_data "github.com/atframework/robot-go/data"
	"github.com/atframework/robot-go/report"
	report_impl "github.com/atframework/robot-go/report/impl"
)

// startSolo 以单节点压测模式运行：本地执行压测，数据写入 Redis（Master 可查看），
// 压测结束后在当前目录生成 {reportID}.html。
func startSolo(flagSet *flag.FlagSet) {
	caseFile := getFlagString(flagSet, "case_file")
	if caseFile == "" {
		fmt.Println("solo mode requires -case_file")
		os.Exit(1)
	}

	repeatedTime := 1
	if v := getFlagString(flagSet, "case_file_repeated"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &repeatedTime); err != nil || n != 1 || repeatedTime < 1 {
			fmt.Println("Invalid case_file_repeated value:", v)
			os.Exit(1)
		}
	}

	redisAddr := getFlagString(flagSet, "redis-addr")
	redisPwd := getFlagString(flagSet, "redis-pwd")

	// 连接 Redis
	redisClient, err := report_impl.NewRedisClient(redisAddr, redisPwd)
	if err != nil {
		fmt.Printf("Connect Redis error: %v\n", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// 生成唯一 ReportID
	reportID := getFlagString(flagSet, "report-id")
	if reportID == "" {
		reportID, err = report_impl.GenerateUniqueReportID(redisClient)
		if err != nil {
			fmt.Printf("Generate report ID error: %v\n", err)
			os.Exit(1)
		}
	}

	log.Printf("[Solo] Starting solo stress test: case=%s repeated=%d reportID=%s redis=%s",
		caseFile, repeatedTime, reportID, redisAddr)

	// 解析 case 文件
	content, err := os.ReadFile(caseFile)
	if err != nil {
		fmt.Printf("Read case file error: %v\n", err)
		os.Exit(1)
	}

	lines, err := parseSoloCaseContent(string(content))
	if err != nil {
		fmt.Printf("Parse case file error: %v\n", err)
		os.Exit(1)
	}
	if len(lines) == 0 {
		fmt.Println("No case lines found in file")
		os.Exit(1)
	}

	redisWriter := report_impl.NewRedisReportWriter(redisClient, "solo")
	startTime := time.Now()

	// 写入初始 meta
	meta := &report.ReportMeta{
		ReportID:  reportID,
		Title:     "Solo Stress Test",
		StartTime: startTime,
		AgentIDs:  []string{"solo"},
		CreatedAt: time.Now(),
	}
	_ = redisWriter.WriteMeta(meta)

	// 收集所有 tracings 和 metrics
	var allTracings []*report.TracingRecord
	var allMetrics []*report.MetricsSeries

	// online_users 指标采集器
	onlineMetrics := report_impl.NewMemoryMetricsCollector()
	onlineMetrics.Register("online_users", func() float64 {
		return float64(user_data.OnlineUserCount())
	})

	errorBreak := false
	for round := 0; round < repeatedTime; round++ {
		for i, params := range lines {
			caseIndex := round*len(lines) + i
			params.CaseIndex = caseIndex

			log.Printf("[Solo] Round %d/%d Case[%d] %s IDs=[%d,%d) QPS=%.1f RunTime=%d",
				round+1, repeatedTime, caseIndex, params.CaseName,
				params.OpenIDStart, params.OpenIDEnd, params.TargetQPS, params.RunTime)

			tracer := report_impl.NewMemoryTracer()
			pressure := report_impl.NewMemoryPressureController()

			_ = onlineMetrics.Flush()
			onlineMetrics.StartAutoCollect(time.Second)

			ctx := context.Background()
			errMsg := robot_case.RunCaseInner(ctx, params, tracer, pressure, false, false)

			onlineMetrics.StopAutoCollect()
			onlineMetrics.Collect()

			// 收集 tracings
			tracings := tracer.Flush()
			allTracings = append(allTracings, tracings...)

			// 收集 pressure metrics
			snapshots := pressure.FlushSnapshots()
			if len(snapshots) > 0 {
				var pressurePts, throttlePts, actualQPSPts []report.MetricsPoint
				for _, s := range snapshots {
					pressurePts = append(pressurePts, report.MetricsPoint{Timestamp: s.Timestamp, Value: float64(s.Level)})
					throttlePts = append(throttlePts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ThrottleRatio})
					actualQPSPts = append(actualQPSPts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ActualQPS})
				}
				allMetrics = append(allMetrics,
					&report.MetricsSeries{Name: "pressure_level", Labels: map[string]string{"agent": "solo", "case": params.CaseName}, Points: pressurePts},
					&report.MetricsSeries{Name: "throttle_ratio", Labels: map[string]string{"agent": "solo", "case": params.CaseName}, Points: throttlePts},
					&report.MetricsSeries{Name: "actual_qps", Labels: map[string]string{"agent": "solo", "case": params.CaseName}, Points: actualQPSPts},
				)
			}

			// 收集 online_users metrics
			onlineSeries := onlineMetrics.Flush()
			for _, s := range onlineSeries {
				if s.Labels == nil {
					s.Labels = make(map[string]string)
				}
				s.Labels["agent"] = "solo"
			}
			allMetrics = append(allMetrics, onlineSeries...)

			if errMsg != "" {
				log.Printf("[Solo] Case[%d] %s completed with error: %s", caseIndex, params.CaseName, errMsg)
				if params.ErrorBreak {
					log.Printf("[Solo] ErrorBreak=true, stopping")
					errorBreak = true
					break
				}
			} else {
				log.Printf("[Solo] Case[%d] %s completed, tracings=%d", caseIndex, params.CaseName, len(tracings))
			}
		}
		if errorBreak {
			break
		}
	}

	endTime := time.Now()

	// 写入 tracings 和 metrics 到 Redis
	if err := redisWriter.WriteTracings(reportID, allTracings); err != nil {
		log.Printf("[Solo] Write tracings to Redis error: %v", err)
	}
	if err := redisWriter.WriteMetrics(reportID, allMetrics); err != nil {
		log.Printf("[Solo] Write metrics to Redis error: %v", err)
	}

	// 清洗 tracings 为 metrics
	cleaned := report.CleanTracingsToMetrics(allTracings)
	allMetrics = append(allMetrics, cleaned...)

	// 计算原始数据大小
	var rawDataSize int64
	if tb, err := json.Marshal(allTracings); err == nil {
		rawDataSize += int64(len(tb))
	}
	if mb, err := json.Marshal(allMetrics); err == nil {
		rawDataSize += int64(len(mb))
	}

	// 更新 meta
	meta.EndTime = endTime
	meta.RawDataSize = rawDataSize
	_ = redisWriter.WriteMeta(meta)

	// 构建 ReportData 并生成 HTML
	data := &report.ReportData{
		Meta:     *meta,
		Tracings: allTracings,
		Metrics:  allMetrics,
	}

	gen := report_impl.NewEChartsHTMLGenerator()
	htmlPath := fmt.Sprintf("%s.html", reportID)
	if err := gen.GenerateToFile(data, htmlPath); err != nil {
		fmt.Printf("Generate HTML report error: %v\n", err)
		os.Exit(1)
	}

	// 更新报告文件大小到 Redis
	if fi, err := os.Stat(htmlPath); err == nil {
		meta.ReportSize = fi.Size()
		_ = redisWriter.WriteMeta(meta)
	}

	log.Printf("[Solo] Report generated: %s", htmlPath)
	log.Printf("[Solo] Duration: %s, Tracings: %d, Metrics series: %d",
		endTime.Sub(startTime).Round(time.Millisecond), len(allTracings), len(allMetrics))

	// 登出所有用户
	user_data.LogoutAllUsers()
}

// parseSoloCaseContent 解析 case 文件内容为参数列表
func parseSoloCaseContent(content string) ([]robot_case.Params, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []robot_case.Params

	for scanner.Scan() {
		raw := scanner.Text()
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
			return nil, err
		}
		params.CaseIndex = len(lines)
		lines = append(lines, params)
	}
	return lines, nil
}
