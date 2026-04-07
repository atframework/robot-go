package atsf4g_go_robot_case

import (
	"strconv"
	"strings"
)

// parseStressLine 解析压测模式的一行参数
// 格式: CaseName ErrorBreak openIdPrefix idStart idEnd targetQPS runTime [args...]
func parseStressLine(cmd []string) (Params, string) {
	if len(cmd) < 7 {
		return Params{}, "Args Error: need at least 7 args (CaseName ErrorBreak openIdPrefix idStart idEnd targetQPS runTime)"
	}
	errorBreak := strings.ToLower(cmd[1]) == "true" || cmd[1] == "1"
	idStart, err := strconv.ParseInt(cmd[3], 10, 64)
	if err != nil {
		return Params{}, "idStart parse error: " + err.Error()
	}
	idEnd, err := strconv.ParseInt(cmd[4], 10, 64)
	if err != nil {
		return Params{}, "idEnd parse error: " + err.Error()
	}
	targetQPS, err := strconv.ParseFloat(cmd[5], 64)
	if err != nil {
		return Params{}, "targetQPS parse error: " + err.Error()
	}
	runTime, err := strconv.ParseInt(cmd[6], 10, 64)
	if err != nil {
		return Params{}, "runTime parse error: " + err.Error()
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
	return params, ""
}

// ParseStressLine 是 parseStressLine 的导出版本，供 master 包解析 case 文件行。
func ParseStressLine(cmd []string) (Params, string) {
	return parseStressLine(cmd)
}
