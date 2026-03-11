package atsf4g_go_robot_case

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lu "github.com/atframework/atframe-utils-go/lang_utility"
	log "github.com/atframework/atframe-utils-go/log"
	base "github.com/atframework/robot-go/base"
	utils "github.com/atframework/robot-go/utils"
)

type CaseFunc func(*TaskActionCase, string, []string) error

var CaseActionActor sync.Map

type TaskActionCase struct {
	base.TaskActionBase
	Fn         CaseFunc
	logHandler func(openId string, format string, a ...any)
	OpenId     string
	Args       []string
}

func (t *TaskActionCase) BeforeYield() {
	channel, _ := CaseActionActor.Load(t.OpenId)
	channel.(chan struct{}) <- struct{}{}
}

func (t *TaskActionCase) AfterYield() {
	channel, ok := CaseActionActor.Load(t.OpenId)
	if !ok {
		newChannel := make(chan struct{}, 1)
		newChannel <- struct{}{}
		channel, _ = CaseActionActor.LoadOrStore(t.OpenId, newChannel)
	}
	<-channel.(chan struct{})
}

func (t *TaskActionCase) HookRun() error {
	t.AfterYield()
	defer t.BeforeYield()
	return t.Fn(t, t.OpenId, t.Args)
}

func (t *TaskActionCase) Log(format string, a ...any) {
	t.logHandler(t.OpenId, format, a...)
}

func init() {
	var _ base.TaskActionImpl = &TaskActionCase{}
	utils.RegisterCommand([]string{"run-case"}, CmdRunCase, "<case name> <openid-prefix> <user-count> <batch-count> <run-time> <args>", "运行用例", AutoCompleteCaseName, 0)
}

type CaseAction struct {
	fun     CaseFunc
	timeout time.Duration
}

var caseMapContainer = make(map[string]CaseAction)

func RegisterCase(name string, fn CaseFunc, timeout time.Duration) {
	caseMapContainer[name] = CaseAction{
		fun:     fn,
		timeout: timeout,
	}
}

func AutoCompleteCaseName(string) []string {
	var res []string
	for k := range caseMapContainer {
		res = append(res, k)
	}
	return res
}

var (
	ProgressBarTotalCount   int64
	ProgressBarCurrentCount atomic.Int64

	FailedCount      atomic.Int64
	TotalFailedCount atomic.Int64
	RefreshFunc      *time.Timer
)

func RefreshProgressBar() {
	countProgressBar := ""
	width := 25
	var progress float64 = 0
	if ProgressBarTotalCount != 0 {
		progress = float64(ProgressBarCurrentCount.Load()) / float64(ProgressBarTotalCount)
		completed := int(progress * float64(width))
		countProgressBar = fmt.Sprintf("[%-*s] %d/%d", width, strings.Repeat("#", completed), ProgressBarCurrentCount.Load(), ProgressBarTotalCount)
		utils.StdoutLog(fmt.Sprintf("Total:%s || Failed:%d             ", countProgressBar, FailedCount.Load()))
		if ProgressBarCurrentCount.Load() >= ProgressBarTotalCount {
			return
		}
	}
	RefreshFunc = time.AfterFunc(time.Second, func() { RefreshProgressBar() })
}

func ClearProgressBar() {
	ProgressBarTotalCount = 0
	ProgressBarCurrentCount.Store(0)
	FailedCount.Store(0)
}

func InitProgressBar(totalCount int64) {
	ProgressBarTotalCount += totalCount
}

func AddProgressBarCount() {
	ProgressBarCurrentCount.Add(1)
}

func RunCaseWait(pendingCase []chan string) error {
	if len(pendingCase) == 0 {
		return nil
	}
	RefreshProgressBar()
	for _, ch := range pendingCase {
		result := <-ch
		if result != "" {
			return fmt.Errorf("Run Case Failed: %s", result)
		}
	}
	RefreshProgressBar()
	if RefreshFunc != nil {
		RefreshFunc.Stop()
		RefreshFunc = nil
	}
	ClearProgressBar()
	return nil
}

func RunCaseFile(caseFile string) error {
	file, err := os.Open(caseFile)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	var pendingCase []chan string
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		args := strings.Fields(line)
		if len(args) == 0 {
			continue
		}

		batchPending := false
		if strings.ToLower(args[len(args)-1]) == "&" {
			args = args[:len(args)-1]
			batchPending = true
		}

		if len(args) == 0 {
			continue
		}

		waitingChan := make(chan string, 1)
		go func() {
			waitingChan <- CmdRunCase(nil, args)
		}()
		pendingCase = append(pendingCase, waitingChan)

		if batchPending {
			continue
		} else {
			err = RunCaseWait(pendingCase)
			if err != nil {
				return err
			}
			pendingCase = pendingCase[:0]
		}
	}

	err = RunCaseWait(pendingCase)
	if err != nil {
		return err
	}
	pendingCase = pendingCase[:0]

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func CmdRunCase(_ base.TaskActionImpl, cmd []string) string {
	if len(cmd) < 5 {
		return "Args Error"
	}

	caseName := cmd[0]
	caseAction, ok := caseMapContainer[caseName]
	if !ok {
		return "Case Not Found"
	}

	openIdPrefix := cmd[1]
	if openIdPrefix == "" {
		return "OpenId Prefix Empty"
	}

	userCount, err := strconv.ParseInt(cmd[2], 10, 32)
	if err != nil {
		return err.Error()
	}

	batchCount, err := strconv.ParseInt(cmd[3], 10, 32)
	if err != nil {
		return err.Error()
	}
	if batchCount <= 0 {
		return "Batch Count Must Greater Than 0"
	}
	if batchCount > userCount {
		batchCount = userCount
	}

	runTime, err := strconv.ParseInt(cmd[4], 10, 32)
	if err != nil {
		return err.Error()
	}

	totalCount := atomic.Int64{}
	totalCount.Store(userCount * runTime)

	timeCounter := sync.Map{}
	openidChannel := make(chan string, userCount)
	for i := int64(0); i < userCount; i++ {
		// 初始化Time统计
		openId := openIdPrefix + strconv.FormatInt(i, 10)
		timeCounter.Store(openId, int32(runTime))
		// 初始化OpenId令牌
		openidChannel <- openId
	}

	InitProgressBar(totalCount.Load())

	caseActionChannel := make(chan *TaskActionCase, batchCount) // 限制并发数

	beginTime := time.Now()

	bufferWriter, _ := log.NewLogBufferedRotatingWriter(nil,
		fmt.Sprintf("../log/%s.%s.%%N.log", caseName, beginTime.Format("15.04.05")), "", 50*1024*1024, 5, time.Second*3, 0)
	logHandler := func(openId string, format string, a ...any) {
		logString := fmt.Sprintf("[%s][%s]: %s", time.Now().Format("2006-01-02 15:04:05.000"), openId, fmt.Sprintf(format, a...))
		bufferWriter.Write(lu.StringtoBytes(logString))
	}
	defer bufferWriter.Close()

	for i := int64(0); i < batchCount; i++ {
		// 创建TaskActionCase
		task := &TaskActionCase{
			TaskActionBase: *base.NewTaskActionBase(caseAction.timeout, "Case Runner"),
			Fn:             caseAction.fun,
			logHandler:     logHandler,
		}
		if len(cmd) > 5 {
			task.Args = cmd[5:]
		}
		task.TaskActionBase.Impl = task
		caseActionChannel <- task
		task.InitOnFinish(func(err error) {
			openId := task.OpenId
			currentCount, _ := timeCounter.Load(openId)
			currentCountInt := currentCount.(int32)
			timeCounter.Store(openId, currentCountInt-1)
			if currentCountInt-1 > 0 {
				// 还有运行次数，继续放回OpenId
				openidChannel <- openId
			}
			AddProgressBarCount()
			if err != nil {
				FailedCount.Add(1)
				TotalFailedCount.Add(1)
				task.Log("Run Case[%s] Failed: %v", task.Name, err)
			}
			caseActionChannel <- task
		})
	}

	mgr := base.NewTaskActionManager()
	finishChannel := make(chan struct{})
	go func() {
		successCount := int64(0)
		for action := range caseActionChannel {
			// 取出OpenId
			openId := <-openidChannel
			action.OpenId = openId
			// 运行TaskAction
			mgr.RunTaskAction(action)
			successCount++
			if successCount >= totalCount.Load() {
				break
			}
		}
		// 等待任务完成
		mgr.WaitAll()
		finishChannel <- struct{}{}
	}()
	<-finishChannel
	useTime := time.Since(beginTime).String()
	logHandler("System", "Case[%s] All Completed, Total Time: %s", caseName, useTime)

	if TotalFailedCount.Load() != 0 {
		return fmt.Sprintf("Complete With %d Failed Args: %v, Total Time: %s", TotalFailedCount.Load(), cmd, useTime)
	}
	utils.StdoutLog(fmt.Sprintf("Complete All Success Args: %v, Total Time: %s", cmd, useTime))
	return ""
}
