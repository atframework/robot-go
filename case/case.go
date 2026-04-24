package atsf4g_go_robot_case

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
	log "github.com/atframework/atframe-utils-go/log"
	base "github.com/atframework/robot-go/base"
	cmd "github.com/atframework/robot-go/cmd"
	user_data "github.com/atframework/robot-go/data"
	report "github.com/atframework/robot-go/report"
	report_impl "github.com/atframework/robot-go/report/impl"
	utils "github.com/atframework/robot-go/utils"
)

var (
	ProgressBarRunning atomic.Int32
	StopedCh           chan struct{}
	StopCh             chan struct{}

	ProgressBarTotalCount   atomic.Int64
	ProgressBarCurrentCount atomic.Int64

	FailedCount      atomic.Int64
	TotalFailedCount atomic.Int64
)

func init() {
	utils.RegisterCommand(cmd.MutableCommandRoot(), []string{"run-case-file"}, CmdRunCaseFile, "<file> <repeated_time>", "运行用例文件", AutoCompleteCaseName, 0)
}

func CmdRunCaseFile(task base.TaskActionImpl, cmd []string) string {
	if len(cmd) < 2 {
		return "Args Error"
	}
	repeatedTime, err := strconv.ParseInt(cmd[1], 10, 32)
	if err != nil {
		return err.Error()
	}

	if runErr := RunCaseFileStandAlone(cmd[0], int32(repeatedTime), nil); runErr != nil {
		return runErr.Error()
	}
	return ""
}

func RefreshProgressBar() {
	countProgressBar := ""
	width := 25
	var progress float64 = 0
	totalCount := ProgressBarTotalCount.Load()
	if totalCount != 0 {
		progress = float64(ProgressBarCurrentCount.Load()) / float64(totalCount)
		completed := int(progress * float64(width))
		countProgressBar = fmt.Sprintf("[%-*s] %d/%d", width, strings.Repeat("#", completed), ProgressBarCurrentCount.Load(), totalCount)
		utils.StdoutLog(fmt.Sprintf("Total:%s || Failed:%d             ", countProgressBar, FailedCount.Load()))
		if ProgressBarCurrentCount.Load() >= totalCount {
			return
		}
	}
}

func InitProgressBar(totalCount int64) {
	ProgressBarTotalCount.Add(totalCount)
}

func AddProgressBarCount() {
	ProgressBarCurrentCount.Add(1)
}

func ShowProgressBar() {
	if ProgressBarRunning.Add(1) == 1 {
		StopedCh = make(chan struct{}, 1)
		StopCh = make(chan struct{}, 1)
		go func() {
			RefreshProgressBar()
			for {
				select {
				case <-time.After(time.Second):
					RefreshProgressBar()
				case <-StopCh:
					RefreshProgressBar()

					ProgressBarTotalCount.Store(0)
					ProgressBarCurrentCount.Store(0)
					FailedCount.Store(0)

					StopedCh <- struct{}{}
					return
				}
			}
		}()
	}
}

func CloseProgressBar() {
	if ProgressBarRunning.Add(-1) == 0 {
		close(StopCh)
		<-StopedCh
	}
}

func runCaseWait(pendingCase []chan string) error {
	if len(pendingCase) == 0 {
		return nil
	}
	for _, ch := range pendingCase {
		result := <-ch
		if result != "" {
			return fmt.Errorf("Run Case Failed: %s", result)
		}
	}
	return nil
}

func RunCaseFileStandAlone(caseFile string, repeatedTime int32, vars map[string]string) error {
	content, err := os.ReadFile(caseFile)
	if err != nil {
		return err
	}

	lines, err := ParseCaseFileContent(SubstituteVariables(string(content), vars))
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}

	for round := int32(0); round < repeatedTime; round++ {
		utils.StdoutLog(fmt.Sprintf("Run Case File: %s, Repeated Time: %d/%d", caseFile, round+1, repeatedTime))

		var pendingCase []chan string
		for _, line := range lines {
			if line.IsControl {
				// 本地模式 控制行不执行，直接跳过
				continue
			}

			pressure := report_impl.NewMemoryPressureController()
			params := line.Stress
			waitingChan := make(chan string, 1)
			go func(p Params) {
				waitingChan <- RunCaseInner(context.Background(), p, nil, pressure, true, true)
			}(params)
			pendingCase = append(pendingCase, waitingChan)

			if !line.BackgroundRunning {
				if err := runCaseWait(pendingCase); err != nil {
					return err
				}
				pendingCase = pendingCase[:0]
			}
		}

		if err := runCaseWait(pendingCase); err != nil {
			return err
		}
	}

	return nil
}

func RunCaseInner(
	ctx context.Context,
	params Params,
	tracer report.Tracer,
	pressure report.PressureController,
	enableLog bool,
	progressBar bool,
) string {
	caseName := params.CaseName
	caseAction, ok := caseMapContainer[caseName]
	if !ok {
		return "Case Not Found"
	}

	userCount := params.UserCount()
	if userCount <= 0 {
		return "ID range is empty"
	}

	runTime := params.RunTime
	if runTime <= 0 {
		runTime = 1
	}

	batchSize := params.UserBatchCount
	if batchSize <= 0 || batchSize > runTime {
		batchSize = runTime
	}

	if batchSize > 5 {
		// 强制限制最大并发数 过高没有意义
		batchSize = 5
	}

	beginTime := time.Now()
	totalCount := userCount * runTime

	qpsCtrl := NewQPSController(params.TargetQPS)
	defer qpsCtrl.Stop()

	// 自适应模式：未设定 QPS 时，由 PressureController 从低到高探测最优速率
	adaptiveMode := params.TargetQPS <= 0 && pressure != nil
	if pressure != nil {
		pressure.SetTargetQPS(params.TargetQPS)
		pressure.Start(time.Second)
		defer pressure.Stop()
		if adaptiveMode {
			qpsCtrl.SetQPS(pressure.EffectiveQPS())
		}
	}

	if progressBar {
		InitProgressBar(totalCount)
		ShowProgressBar()
		defer CloseProgressBar()
	}

	var logHandler func(openId string, format string, a ...any) = nil
	if enableLog {
		bufferWriter, _ := log.NewLogBufferedRotatingWriter(nil,
			fmt.Sprintf("../log/%d.%s.%s.%%N.log", params.CaseIndex, caseName, beginTime.Format("15.04.05")), "", 10*1024*1024, 3, time.Second*3, 0)
		logHandler = func(openId string, format string, a ...any) {
			logString := fmt.Sprintf("[%s][%s]: %s", time.Now().Format("2006-01-02 15:04:05.000"), openId, fmt.Sprintf(format, a...))
			bufferWriter.Write(lu.StringtoBytes(logString))
		}
		defer func() {
			bufferWriter.Close()
			bufferWriter.AwaitClose()
		}()

		logHandler("System", "Case[%s] Start, Index[%d] Users: %d, QPS: %.1f, RunTime: %d, ErrorBreak: %v",
			caseName, params.CaseIndex, userCount, params.TargetQPS, runTime, params.ErrorBreak)
	}

	var failedCount atomic.Int64
	var errorBreakState atomic.Pointer[error]

	// Worker 数量：GOMAXPROCS/2
	workers := runtime.GOMAXPROCS(0) / 2
	if workers < 1 {
		workers = 1
	}

	type workerData struct {
		workerIndex       int
		totalTaskCount    int64
		finishedTaskCount int64
		userHolderChannel chan *user_data.UserHolder
	}

	type userCaseData struct {
		openId            string
		dispatchTaskCount atomic.Int64
		totalTaskCount    int64
	}
	type userPrivateData struct {
		index int32
	}

	if workers > int(userCount) {
		workers = int(userCount)
	}

	// 初始化 worker 数据结构，每个 worker 持有一个 openId channel，避免竞争
	workerDatas := make([]*workerData, workers)
	for i := 0; i < workers; i++ {
		workerDatas[i] = &workerData{
			workerIndex:       i,
			userHolderChannel: make(chan *user_data.UserHolder, batchSize*((userCount/int64(workers))+1)),
		}
	}

	// 格式化所有 openId 字符串 分配入worker
	userCaseDatas := make([]*userCaseData, userCount)
	for i := int64(0); i < userCount; i++ {
		userCaseDatas[i] = &userCaseData{}
		userCaseDatas[i].openId = params.OpenIDPrefix + strconv.FormatInt(params.OpenIDStart+i, 10)
		workerIndex := int(i % int64(workers))
		userCaseDatas[i].totalTaskCount = runTime

		userHolder := user_data.UserContainerGetUser(userCaseDatas[i].openId)
		userHolder.Index.Store(int32(i))
		// 先放入1次任务
		workerDatas[workerIndex].userHolderChannel <- userHolder
		userCaseDatas[i].dispatchTaskCount.Add(1)
		workerDatas[workerIndex].totalTaskCount += runTime
	}

	// 如果并发度高于1 放入后续User
	if batchSize > 1 {
		for range batchSize - 1 {
			for i := int64(0); i < userCount; i++ {
				workerIndex := int(i % int64(workers))
				userHolder := user_data.UserContainerGetUser(userCaseDatas[i].openId)
				workerDatas[workerIndex].userHolderChannel <- userHolder
				userCaseDatas[i].dispatchTaskCount.Add(1)
			}
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerData *workerData) {
			defer wg.Done()

			mgr := base.NewTaskActionManagerWithPool(4096)
			defer mgr.ReleasePool()

			var runTaskCount int64

			taskActionPool := sync.Pool{
				New: func() any {
					task := &TaskActionCase{
						TaskActionBase: *base.NewTaskActionBase(caseAction.timeout, "Case Runner Worker"),
						Fn:             caseAction.fun,
						logHandler:     logHandler,
						Args:           params.ExtraArgs,
						NeedLog:        enableLog,
					}
					if len(params.ExtraArgs) > 0 {
						task.Args = params.ExtraArgs
					}
					task.TaskActionBase.Impl = task
					return task
				},
			}

			onFinishFunc := func(task base.TaskActionImpl, err error) {
				caseData := userCaseDatas[task.(*TaskActionCase).UserHolder.Index.Load()]
				// 先增加已完成的任务数，再决定是否继续分配任务，避免竞争
				finishTaskCount := caseData.dispatchTaskCount.Add(1)
				if finishTaskCount <= caseData.totalTaskCount {
					workerData.userHolderChannel <- task.(*TaskActionCase).UserHolder
				}
				if progressBar {
					AddProgressBarCount()
				}
				if err != nil {
					failedCount.Add(1)
					FailedCount.Add(1)
					TotalFailedCount.Add(1)
					task.Log("Case[%s] Failed: %v", caseName, err)
					if params.ErrorBreak {
						errValue := err
						errorBreakState.CompareAndSwap(nil, &errValue)
					}
				}
				if adaptiveMode {
					latency := time.Since(task.(*TaskActionCase).DispatchedAt)
					pressure.RecordLatency(latency)
					pressure.DonePending()
				}
				mgr.OnTaskFinish(task.GetTaskId())
				taskActionPool.Put(task)
			}

			for {
				if errorBreakState.Load() != nil {
					return
				}
				if ctx.Err() != nil {
					return
				}

				userHolder := <-workerData.userHolderChannel
				// 通过worker分片openid，避免竞争
				task := taskActionPool.Get().(*TaskActionCase)
				task.ResetForReuse()
				task.UserHolder = userHolder
				task.InitOnFinish(onFinishFunc)

				// QPS 控制: 自适应模式下由 PressureController 驱动速率
				if adaptiveMode {
					qpsCtrl.SetQPS(math.Max(pressure.EffectiveQPS(), 1))
				}
				qpsCtrl.Acquire()

				if adaptiveMode {
					pressure.AddPending()
					task.DispatchedAt = time.Now()
				}

				// 打点
				if tracer != nil {
					task.TracerEntry = tracer.NewEntry(caseName).Start()
				}

				mgr.RunTaskAction(task)
				runTaskCount++
				if runTaskCount >= workerData.totalTaskCount {
					break
				}
			}
			mgr.WaitAll()
		}(workerDatas[w])
	}
	wg.Wait()

	useTime := time.Since(beginTime).String()
	if enableLog {
		logHandler("System", "Case[%s:%d]  Completed, Total Time: %s", caseName, params.CaseIndex, useTime)
	}

	if ctx.Err() != nil {
		return fmt.Sprintf("Case[%s:%d] Cancelled, Total Time: %s", caseName, params.CaseIndex, useTime)
	}

	if failedCount.Load() != 0 {
		var caseError error
		if errorBreakPtr := errorBreakState.Load(); errorBreakPtr != nil {
			caseError = *errorBreakPtr
		}
		return fmt.Sprintf("Case[%s:%d] Complete With %d Failed, Total Time: %s, Error: %v", caseName, params.CaseIndex, failedCount.Load(), useTime, caseError)
	}
	utils.StdoutLog(fmt.Sprintf("Case[%s:%d] All Success, Total Time: %s", caseName, params.CaseIndex, useTime))
	return ""
}
