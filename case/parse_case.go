package atsf4g_go_robot_case

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// ParseLine 解析一行参数
// 格式: CaseName ErrorBreak IDPrefix IDStart IDEnd TargetQPS UserBatchCount RunTime [args...]
// ErrorBreak 固定为 true，OpenIDStart 固定为 0，OpenIDPrefix 由全局参数指定
func ParseLine(cmd []string) (Params, error) {
	if len(cmd) < 8 {
		return Params{}, fmt.Errorf("Args Error: need at least 8 args (CaseName ErrorBreak IDPrefix IDStart IDEnd TargetQPS UserBatchCount RunTime)")
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
	userBatchCount, err := strconv.ParseInt(cmd[6], 10, 64)
	if err != nil {
		return Params{}, fmt.Errorf("userBatchCount parse error: %w", err)
	}
	runTime, err := strconv.ParseInt(cmd[7], 10, 64)
	if err != nil {
		return Params{}, fmt.Errorf("runTime parse error: %w", err)
	}
	params := Params{
		CaseName:       cmd[0],
		ErrorBreak:     errorBreak,
		OpenIDPrefix:   cmd[2],
		OpenIDStart:    idStart,
		OpenIDEnd:      idEnd,
		TargetQPS:      targetQPS,
		UserBatchCount: userBatchCount,
		RunTime:        runTime,
	}
	if len(cmd) > 8 {
		params.ExtraArgs = cmd[8:]
	}
	return params, nil
}

// ParseCaseFileContent 解析 case 文件内容，返回混合了控制指令和压测行的统一行列表。
// 支持 # 注释、行尾 & 后台标记、@ 控制指令前缀。
// &后台标记的任务知道最后一个非后台标记的任务 OpenIDPrifix 都不能改变
func ParseCaseFileContent(content string) ([]CaseFileLine, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))

	var lines []CaseFileLine
	OpenIdPrefixCheck := ""
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

		// 检测行尾 & 后台标记
		background := false
		if len(args) > 0 && strings.ToLower(args[len(args)-1]) == "&" {
			background = true
			args = args[:len(args)-1]
		}
		if len(args) == 0 {
			continue
		}

		caseIndex := len(lines)

		if IsControlLine(args[0]) {
			// 控制指令行（两种模式均使用相同格式）
			cp, err := ParseControlLine(args)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", caseIndex, err)
			}
			cp.CaseIndex = caseIndex
			cp.Background = background
			lines = append(lines, CaseFileLine{
				IsControl:         true,
				BackgroundRunning: background,
				Control:           cp,
			})
		} else {
			params, err := ParseLine(args)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", caseIndex, err)
			}
			if OpenIdPrefixCheck != "" && params.OpenIDPrefix != OpenIdPrefixCheck {
				return nil, fmt.Errorf("line %d: OpenIDPrefix %s does not match previous lines' prefix %s", caseIndex, params.OpenIDPrefix, OpenIdPrefixCheck)
			}
			if background {
				OpenIdPrefixCheck = params.OpenIDPrefix
			} else {
				OpenIdPrefixCheck = ""
			}
			params.CaseIndex = caseIndex
			lines = append(lines, CaseFileLine{
				IsControl:         false,
				BackgroundRunning: background,
				Stress:            params,
			})
		}
	}
	return lines, nil
}
