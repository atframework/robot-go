package atsf4g_go_robot_user

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	robot_case "github.com/atframework/robot-go/case"
	user_data "github.com/atframework/robot-go/data"
	"github.com/atframework/robot-go/report"
	report_impl "github.com/atframework/robot-go/report/impl"
	"github.com/redis/go-redis/v9"
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
	enableRedis := redisAddr != ""

	// 连接 Redis
	var redisClient *redis.Client
	if enableRedis {
		redisClient, err := report_impl.NewRedisClient(redisAddr, redisPwd)
		if err != nil {
			fmt.Printf("Connect Redis error: %v\n", err)
			os.Exit(1)
		}
		defer redisClient.Close()
	}

	// 生成唯一 ReportID
	reportID := getFlagString(flagSet, "report-id")
	if reportID == "" {
		var err error
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

	lines, err := robot_case.ParseCaseFileContent(robot_case.SubstituteVariables(string(content), GetSetVars(flagSet)))
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

	// 收集所有 tracings 和 metrics（增量累积，定时写入 Redis）
	var (
		accMu       sync.Mutex
		allTracings []*report.TracingRecord
		allMetrics  []*report.MetricsSeries
	)

	// online_users 指标采集器
	onlineMetrics := report_impl.NewMemoryMetricsCollector()
	onlineMetrics.Register("online_users", func() float64 {
		return float64(user_data.OnlineUserCount())
	})

	type bgResult struct {
		err        error
		errorBreak bool
		caseName   string
		caseIndex  int
	}
	var bgTasks []chan bgResult

	waitRunningTask := func() error {
		for _, ch := range bgTasks {
			res := <-ch
			if res.err != nil {
				log.Printf("[Solo] Case[%d] %s completed with error: %v", res.caseIndex, res.caseName, res.err)
				if res.errorBreak {
					return fmt.Errorf("case[%d] %s: %w", res.caseIndex, res.caseName, res.err)
				}
			} else {
				log.Printf("[Solo] Case[%d] %s completed", res.caseIndex, res.caseName)
			}
		}
		bgTasks = bgTasks[:0]
		return nil
	}

	// runSoloCase 封装单个 case 的完整执行流程（tracer、pressure、flush、RunCaseInner）
	runSoloCase := func(caseIndex int, params robot_case.Params) error {
		log.Printf("[Solo] Case[%d] %s IDs=[%d,%d) QPS=%.1f RunTime=%d",
			caseIndex, params.CaseName,
			params.OpenIDStart, params.OpenIDEnd, params.TargetQPS, params.RunTime)

		tracer := report_impl.NewMemoryTracer()
		pressure := report_impl.NewMemoryPressureController()

		_ = onlineMetrics.Flush()
		onlineMetrics.StartAutoCollect(time.Second)

		// ── 定时刷新闭包：排空 tracer/pressure/onlineMetrics → Redis + 本地累积 ──
		caseName := params.CaseName
		flushOnce := func() {
			tracings := tracer.Flush()

			var series []*report.MetricsSeries

			snapshots := pressure.FlushSnapshots()
			if len(snapshots) > 0 {
				var levelPts, qpsPts, latencyPts []report.MetricsPoint
				for _, s := range snapshots {
					levelPts = append(levelPts, report.MetricsPoint{Timestamp: s.Timestamp, Value: float64(s.Level)})
					qpsPts = append(qpsPts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.ActualQPS})
					if s.LatencyP50Ms > 0 {
						latencyPts = append(latencyPts, report.MetricsPoint{Timestamp: s.Timestamp, Value: s.LatencyP50Ms})
					}
				}
				series = append(series,
					&report.MetricsSeries{Name: "pressure_level", Labels: map[string]string{"agent": "solo", "case": caseName}, Points: levelPts},
					&report.MetricsSeries{Name: "actual_qps", Labels: map[string]string{"agent": "solo", "case": caseName}, Points: qpsPts},
				)
				if len(latencyPts) > 0 {
					series = append(series, &report.MetricsSeries{Name: "latency_p50_ms", Labels: map[string]string{"agent": "solo", "case": caseName}, Points: latencyPts})
				}
			}

			onlineSeries := onlineMetrics.Flush()
			for _, s := range onlineSeries {
				if s.Labels == nil {
					s.Labels = make(map[string]string)
				}
				s.Labels["agent"] = "solo"
			}
			series = append(series, onlineSeries...)

			// 写入 Redis（增量 RPush）
			if len(tracings) > 0 {
				if err := redisWriter.WriteTracings(reportID, tracings); err != nil {
					log.Printf("[Solo] flush tracings error: %v", err)
				}
			}
			if len(series) > 0 {
				if err := redisWriter.WriteMetrics(reportID, series); err != nil {
					log.Printf("[Solo] flush metrics error: %v", err)
				}
			}

			// 本地累积（用于最终 HTML 生成）
			accMu.Lock()
			allTracings = append(allTracings, tracings...)
			allMetrics = append(allMetrics, series...)
			accMu.Unlock()
		}

		// 启动定时刷新协程（类似 Agent 模式，每 5 秒刷新一次）
		flushStopCh := make(chan struct{})
		flushDone := make(chan struct{})
		go func() {
			defer close(flushDone)
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					flushOnce()
					meta.EndTime = time.Now()
					_ = redisWriter.WriteMeta(meta)
				case <-flushStopCh:
					return
				}
			}
		}()

		ctx := context.Background()
		errMsg := robot_case.RunCaseInner(ctx, params, tracer, pressure, false, true)

		// 停止定时刷新
		close(flushStopCh)
		<-flushDone

		onlineMetrics.StopAutoCollect()
		onlineMetrics.Collect()

		// 最终刷新（捕获剩余未被 ticker 捞到的数据）
		flushOnce()
		if errMsg != "" {
			return fmt.Errorf("case %s error: %s", caseName, errMsg)
		}
		return nil
	}

	errorBreak := false
	for round := 0; round < repeatedTime; round++ {
		for i, line := range lines {
			if errorBreak {
				break
			}
			caseIndex := round*len(lines) + i

			// 控制指令：Solo 模式直接本地执行（等同于 Agent 执行）
			if line.IsControl {
				cp := line.Control
				cp.CaseIndex = caseIndex

				ch := make(chan bgResult, 1)
				bgTasks = append(bgTasks, ch)
				go func(cp robot_case.ControlParams, idx int) {
					log.Printf("[Solo] Round %d/%d Control[%d] @%s args=%v",
						round+1, repeatedTime, idx, cp.Name, cp.Args)
					err := robot_case.RunControlInner(context.Background(), cp)
					ch <- bgResult{err: err, caseName: "@" + cp.Name, caseIndex: idx, errorBreak: line.Control.ErrorBreak}
				}(cp, caseIndex)
				if !line.BackgroundRunning {
					errorBreak = waitRunningTask() != nil
				}
				continue
			}

			params := line.Stress
			params.CaseIndex = caseIndex

			// 以后台方式启动 case，非 & 行启动后立即等待所有 pending 任务完成
			ch := make(chan bgResult, 1)
			bgTasks = append(bgTasks, ch)
			go func(params robot_case.Params, idx int) {
				log.Printf("[Solo] Round %d/%d Case[%d] %s IDs=[%d,%d) QPS=%.1f RunTime=%d",
					round+1, repeatedTime, idx, params.CaseName,
					params.OpenIDStart, params.OpenIDEnd, params.TargetQPS, params.RunTime)
				err := runSoloCase(idx, params)
				ch <- bgResult{err: err, caseName: params.CaseName, caseIndex: idx, errorBreak: params.ErrorBreak}
			}(params, caseIndex)
			if !line.BackgroundRunning {
				errorBreak = waitRunningTask() != nil
			}
		}
		if errorBreak {
			break
		}
		// 每轮结束时等待剩余后台任务
		errorBreak = waitRunningTask() != nil
		if errorBreak {
			break
		}
	}

	endTime := time.Now()

	// 清洗 tracings 为辅助指标（QPS / 延迟统计等）
	cleaned := report.CleanTracingsToMetrics(allTracings)
	allMetrics = append(allMetrics, cleaned...)

	// 最终 meta 更新
	meta.EndTime = endTime
	_ = redisWriter.WriteMeta(meta)

	// 生成 HTML 报告
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
