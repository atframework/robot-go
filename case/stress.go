package atsf4g_go_robot_case

import (
	"fmt"
	"strconv"
	"strings"
)

// parseStressLine 解析压测模式的一行参数
// 格式: CaseName ErrorBreak openIdPrefix idStart idEnd targetQPS runTime [args...]
func ParseStressLine(cmd []string) (Params, error) {
	if len(cmd) < 7 {
		return Params{}, fmt.Errorf("Args Error: need at least 7 args (CaseName ErrorBreak openIdPrefix idStart idEnd targetQPS runTime)")
	}
	errorBreak := strings.ToLower(cmd[1]) == "true" || cmd[1] == "1"
	idStart, err := strconv.ParseInt(cmd[3], 10, 64)
	if err != nil {
		return Params{}, fmt.Errorf("idStart parse error: %w", err)
	}
	idEnd, err := strconv.ParseInt(cmd[4], 10, 64)
	if err != nil {
		return Params{}, fmt.Errorf("idEnd parse error: %w", err)
	}
	targetQPS, err := strconv.ParseFloat(cmd[5], 64)
	if err != nil {
		return Params{}, fmt.Errorf("targetQPS parse error: %w", err)
	}
	runTime, err := strconv.ParseInt(cmd[6], 10, 64)
	if err != nil {
		return Params{}, fmt.Errorf("runTime parse error: %w", err)
	}
	params := Params{
		CaseName:     cmd[0],
		ErrorBreak:   errorBreak,
		OpenIDPrefix: cmd[2],
		OpenIDStart:  idStart,
		OpenIDEnd:    idEnd,
		TargetQPS:    targetQPS,
		RunTime:      runTime,
	}
	if len(cmd) > 7 {
		params.ExtraArgs = cmd[7:]
	}
	return params, nil
}
