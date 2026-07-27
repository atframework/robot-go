package atsf4g_go_robot_case

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// ControlDispatchMode 控制指令在分布式模式下的分发策略
type ControlDispatchMode int

const (
	// ControlDispatchAll 所有目标 Agent 都执行该控制指令
	ControlDispatchAll ControlDispatchMode = iota
	// ControlDispatchRandom 随机选择一个 Agent 执行该控制指令
	ControlDispatchRandom
)

// ControlFunc 控制指令的执行函数
// ctx: 上下文，可用于取消
// args: 控制指令的参数列表（不含 @指令名）
type ControlFunc func(ctx context.Context, args []string) error

// ControlAction 已注册的控制指令
type ControlAction struct {
	Fn           ControlFunc
	DispatchMode ControlDispatchMode
}

// ControlParams 控制指令解析后的参数
type ControlParams struct {
	// 控制指令名称（不含 @ 前缀）
	Name string `json:"name"`
	// 解析 case 文件时的行号，从 0 开始
	CaseIndex int `json:"case_index"`
	// 错误是否打断：true 时遇到错误即停止后续行
	ErrorBreak bool `json:"error_break"`
	// 是否后台执行（case 文件行尾是否有 &）
	Background bool `json:"background"`
	// 额外参数
	Args []string `json:"args,omitempty"`
}

// controlMapContainer 控制指令注册表
var controlMapContainer = make(map[string]ControlAction)

func init() {
	RegisterControl("cmd", runCommandControl, ControlDispatchAll)
}

// runCommandControl 以“可执行文件 [参数...]”形式执行外部命令。
// 不经 shell 解析，参数会按 case 文件拆分后的边界传递给目标程序。
func runCommandControl(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("cmd control requires a command: @cmd ErrorBreak command [args...]")
	}

	command := exec.CommandContext(ctx, args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if len(output) > 0 {
		log.Printf("[Control] cmd output:\n%s", output)
	}
	if err != nil {
		return fmt.Errorf("run command %q: %w", args[0], err)
	}
	return nil
}

// RegisterControl 注册一个控制指令
// name: 指令名称（不含 @ 前缀，例如 "reboot"、"pprof"）
// fn: 执行函数
// dispatchMode: 分布式分发策略
func RegisterControl(name string, fn ControlFunc, dispatchMode ControlDispatchMode) {
	controlMapContainer[name] = ControlAction{
		Fn:           fn,
		DispatchMode: dispatchMode,
	}
}

// GetControlAction 根据名称获取已注册的控制指令，未找到返回 nil
func GetControlAction(name string) *ControlAction {
	if action, ok := controlMapContainer[name]; ok {
		return &action
	}
	return nil
}

// AutoCompleteControlName 返回所有已注册的控制指令名称（用于命令补全）
func AutoCompleteControlName(_ string) []string {
	var res []string
	for k := range controlMapContainer {
		res = append(res, "@"+k)
	}
	return res
}

// IsControlLine 判断一行是否是控制指令（以 @ 开头）
func IsControlLine(firstArg string) bool {
	return strings.HasPrefix(firstArg, "@")
}

// ParseControlLine 解析一行控制指令
// 格式: @Name ErrorBreak [args...]
// 最少需要 2 个字段：@Name ErrorBreak
func ParseControlLine(args []string) (ControlParams, error) {
	if len(args) < 2 {
		return ControlParams{}, fmt.Errorf("control instruction needs at least 2 args: @Name ErrorBreak [args...]")
	}

	name := strings.TrimPrefix(args[0], "@")
	if name == "" {
		return ControlParams{}, fmt.Errorf("control instruction name is empty")
	}

	errorBreak := strings.ToLower(args[1]) == "true" || args[1] == "1"

	params := ControlParams{
		Name:       name,
		ErrorBreak: errorBreak,
	}
	if len(args) > 2 {
		params.Args = args[2:]
	}
	return params, nil
}

// RunControlInner 在本地执行一个控制指令（Agent / Solo 模式调用）
func RunControlInner(ctx context.Context, params ControlParams) error {
	action := GetControlAction(params.Name)
	if action == nil {
		return fmt.Errorf("control instruction not found: @%s", params.Name)
	}

	log.Printf("[Control] Executing @%s args=%v", params.Name, params.Args)
	if err := action.Fn(ctx, params.Args); err != nil {
		return fmt.Errorf("control @%s failed: %w", params.Name, err)
	}
	log.Printf("[Control] @%s completed", params.Name)
	return nil
}
