package atsf4g_go_robot_case

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
	log "github.com/atframework/atframe-utils-go/log"
	base "github.com/atframework/robot-go/base"
	report "github.com/atframework/robot-go/report"
	utils "github.com/atframework/robot-go/utils"
)

// parseStressLine 解析压测模式的一行参数
// 格式: CaseName ErrorBreak openIdPrefix idStart idEnd batchCount targetQPS runTime [args...]
func parseStressLine(cmd []string) (StressParams, string) {
	if len(cmd) < 8 {
		return StressParams{}, "Args Error: need at least 8 args (CaseName ErrorBreak openIdPrefix idStart idEnd batchCount targetQPS runTime)"
	}
	errorBreak := strings.ToLower(cmd[1]) == "true" || cmd[1] == "1"
	idStart, err := strconv.ParseInt(cmd[3], 10, 64)
	if err != nil {
		return StressParams{}, "idStart parse error: " + err.Error()
	}
	idEnd, err := strconv.ParseInt(cmd[4], 10, 64)
	if err != nil {
		return StressParams{}, "idEnd parse error: " + err.Error()
	}
	batchCount, err := strconv.ParseInt(cmd[5], 10, 64)
	if err != nil {
		return StressParams{}, "batchCount parse error: " + err.Error()
	}
	targetQPS, err := strconv.ParseFloat(cmd[6], 64)
	if err != nil {
		return StressParams{}, "targetQPS parse error: " + err.Error()
	}
	runTime, err := strconv.ParseInt(cmd[7], 10, 64)
	if err != nil {
		return StressParams{}, "runTime parse error: " + err.Error()
	}
	params := StressParams{
		CaseName:     cmd[0],
		ErrorBreak:   errorBreak,
		OpenIDPrefix: cmd[2],
		OpenIDStart:  idStart,
		OpenIDEnd:    idEnd,
		BatchCount:   batchCount,
		TargetQPS:    targetQPS,
		RunTime:      runTime,
	}
	if len(cmd) > 8 {
		params.ExtraArgs = cmd[8:]
	}
	return params, ""
}

// ParseStressLine 是 parseStressLine 的导出版本，供 master 包解析 case 文件行。
func ParseStressLine(cmd []string) (StressParams, string) {
	return parseStressLine(cmd)
}

// RunCaseStressWithContext 与 RunCaseStress 相同，但支持通过 context 取消执行。
func RunCaseStressWithContext(
	ctx context.Context,
	params StressParams,
	tracer report.Tracer,
	pressure report.PressureController,
	enableLog bool,
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

	batchCount := params.BatchCount
	if batchCount <= 0 {
		batchCount = 1
	}
	if batchCount > userCount {
		batchCount = userCount
	}

	runTime := params.RunTime
	if runTime <= 0 {
		runTime = 1
	}

	beginTime := time.Now()
	totalCount := userCount * runTime

	qpsCtrl := NewQPSController(params.TargetQPS)

	// 如果有 pressure controller，将 qps 控制与压力检测联动
	if pressure != nil {
		pressure.SetTargetQPS(params.TargetQPS)
		pressure.Start(time.Second)
		defer pressure.Stop()
	}

	// 构造账号池
	openidChan := make(chan string, userCount)
	timeCounter := sync.Map{}
	for i := params.OpenIDStart; i < params.OpenIDEnd; i++ {
		openId := params.OpenIDPrefix + strconv.FormatInt(i, 10)
		timeCounter.Store(openId, int32(runTime))
		openidChan <- openId
	}

	InitProgressBar(totalCount)

	var logHandler func(openId string, format string, a ...any) = nil
	if enableLog {
		bufferWriter, _ := log.NewLogBufferedRotatingWriter(nil,
			fmt.Sprintf("../log/stress.%s.%s.%%N.log", caseName, beginTime.Format("15.04.05")), "", 50*1024*1024, 3, time.Second*3, 0)
		logHandler = func(openId string, format string, a ...any) {
			logString := fmt.Sprintf("[%s][%s]: %s", time.Now().Format("2006-01-02 15:04:05.000"), openId, fmt.Sprintf(format, a...))
			bufferWriter.Write(lu.StringtoBytes(logString))
		}
		defer func() {
			bufferWriter.Close()
			bufferWriter.AwaitClose()
		}()

		logHandler("System", "StressCase[%s] Start, Users: %d, Batch: %d, QPS: %.1f, RunTime: %d, ErrorBreak: %v",
			caseName, userCount, batchCount, params.TargetQPS, runTime, params.ErrorBreak)
	}

	caseActionChannel := make(chan *TaskActionCase, batchCount)
	var stressFailedCount atomic.Int64
	var errorBreakTriggered atomic.Bool

	for i := int64(0); i < batchCount; i++ {
		task := &TaskActionCase{
			TaskActionBase: *base.NewTaskActionBase(caseAction.timeout, "Stress Runner"),
			Fn:             caseAction.fun,
			logHandler:     logHandler,
		}
		if len(params.ExtraArgs) > 0 {
			task.Args = params.ExtraArgs
		}
		task.TaskActionBase.Impl = task
		caseActionChannel <- task
		task.InitOnFinish(func(err error) {
			openId := task.OpenId
			current, _ := timeCounter.Load(openId)
			currentInt := current.(int32)
			timeCounter.Store(openId, currentInt-1)
			if currentInt-1 > 0 {
				openidChan <- openId
			}
			AddProgressBarCount()
			if err != nil {
				stressFailedCount.Add(1)
				FailedCount.Add(1)
				TotalFailedCount.Add(1)
				task.Log("StressCase[%s] Failed: %v", caseName, err)
				if params.ErrorBreak {
					errorBreakTriggered.Store(true)
				}
			}
			if pressure != nil {
				pressure.DonePending()
			}
			caseActionChannel <- task
		})
	}

	mgr := base.NewTaskActionManager()
	finishChannel := make(chan struct{})
	go func() {
		successCount := int64(0)
		for action := range caseActionChannel {
			// ErrorBreak: 遇到错误时停止调度新任务
			if errorBreakTriggered.Load() {
				caseActionChannel <- action
				break
			}
			// Context 取消：外部请求停止
			if ctx.Err() != nil {
				caseActionChannel <- action
				break
			}

			openId := <-openidChan
			action.OpenId = openId

			// QPS 控制: 联动 pressure
			if pressure != nil && params.TargetQPS > 0 {
				effective := pressure.EffectiveQPS()
				qpsCtrl.SetQPS(math.Max(effective, 1))
			}
			qpsCtrl.Acquire()

			if pressure != nil {
				pressure.AddPending()
			}

			// 打点: 先重置 Fn 再包装，防止任务复用时闭包嵌套累积
			action.Fn = caseAction.fun
			if tracer != nil {
				entry := tracer.NewEntry(caseName).Start()
				origFn := action.Fn
				action.Fn = func(t *TaskActionCase, oid string, args []string) error {
					err := origFn(t, oid, args)
					entry.EndWithError(err)
					return err
				}
			}

			mgr.RunTaskAction(action)
			successCount++
			if successCount >= totalCount {
				break
			}
		}
		mgr.WaitAll()
		finishChannel <- struct{}{}
	}()
	<-finishChannel

	useTime := time.Since(beginTime).String()
	if enableLog {
		logHandler("System", "StressCase[%s] Completed, Total Time: %s", caseName, useTime)
	}

	if ctx.Err() != nil {
		return fmt.Sprintf("StressCase[%s] Cancelled, Total Time: %s", caseName, useTime)
	}

	if stressFailedCount.Load() != 0 {
		return fmt.Sprintf("StressCase[%s] Complete With %d Failed, Total Time: %s", caseName, stressFailedCount.Load(), useTime)
	}
	utils.StdoutLog(fmt.Sprintf("StressCase[%s] All Success, Total Time: %s", caseName, useTime))
	return ""
}
