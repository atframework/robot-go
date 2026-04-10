package impl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/atframework/robot-go/report"
)

// EChartsHTMLGenerator 使用 ECharts CDN 生成独立 HTML 报告
type EChartsHTMLGenerator struct{}

func NewEChartsHTMLGenerator() *EChartsHTMLGenerator {
	return &EChartsHTMLGenerator{}
}

func (g *EChartsHTMLGenerator) Generate(data *report.ReportData) ([]byte, error) {
	td := g.buildTemplateData(data)
	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

func (g *EChartsHTMLGenerator) GenerateToFile(data *report.ReportData, outputPath string) error {
	html, err := g.Generate(data)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, html, 0640)
}

// --- 模板数据结构 ---

type caseStat struct {
	Name       string  `json:"name"`
	Total      int     `json:"total"`
	Success    int     `json:"success"`
	Failed     int     `json:"failed"`
	AvgMs      float64 `json:"avgMs"`
	VarianceMs int64   `json:"varianceMs"`
	MinMs      int64   `json:"minMs"`
	MaxMs      int64   `json:"maxMs"`
}

type metricsSection struct {
	CaseGroup  string
	ShowHeader bool
	Name       string
	Labels     string
	Times      template.JS
	Values     template.JS
}

// metricsDataEntry 为指标下拉筛选提供的 JS 可用数据
type metricsDataEntry struct {
	Name       string    `json:"name"`
	CaseGroup  string    `json:"caseGroup,omitempty"`
	AgentGroup string    `json:"agentGroup,omitempty"`
	Labels     string    `json:"labels,omitempty"`
	Times      []string  `json:"times"`
	Values     []float64 `json:"values"`
}

// chartSeriesData 按秒聚合的单条 case 时间序列
type chartSeriesData struct {
	Name       string           `json:"name"`
	MinTime    string           `json:"minTime,omitempty"`
	MaxTime    string           `json:"maxTime,omitempty"`
	QPS        []int            `json:"qps"`
	Success    []int            `json:"success"`
	Failed     []int            `json:"failed"`
	AvgMs      []float64        `json:"avgMs"`
	VarianceMs []int64          `json:"varianceMs"`
	Errors     []errorCodeEntry `json:"errors,omitempty"`
}

type chartData struct {
	TimeLabels []string          `json:"timeLabels"`
	StartEpoch int64             `json:"startEpoch,omitempty"`
	Series     []chartSeriesData `json:"series"`
}

type errorCodeEntry struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type templateData struct {
	Title     string
	ReportID  string
	StartTime string
	EndTime   string
	CreatedAt string
	// 全局汇总
	TotalReqs   int
	SuccessReqs int
	FailedReqs  int
	AvgMs       float64
	VarianceMs  float64
	// 数据大小
	RawDataSizeStr string
	ReportSizeStr  string
	// 分 Case 统计
	CaseStats []caseStat
	// JSON（供 JS 读取）
	ChartDataJSON  template.JS
	ErrorCodesJSON template.JS
	// Metrics
	OnlineUsersJSON template.JS      // 在线用户多系列 JSON，显示在 QPS 图前
	MetricsSections []metricsSection // 其他指标（仅用于判断是否有非 online 指标）
	MetricsJSON     template.JS      // 全部非 online_users 指标的 JSON，供 JS 动态渲染
}

func formatBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
}

func (g *EChartsHTMLGenerator) buildTemplateData(data *report.ReportData) *templateData {
	td := &templateData{
		Title:           data.Meta.Title,
		ReportID:        data.Meta.ReportID,
		StartTime:       data.Meta.StartTime.Format("2006-01-02 15:04:05"),
		EndTime:         data.Meta.EndTime.Format("2006-01-02 15:04:05"),
		CreatedAt:       data.Meta.CreatedAt.Format("2006-01-02 15:04:05"),
		ChartDataJSON:   template.JS(`{"timeLabels":[],"series":[]}`),
		ErrorCodesJSON:  template.JS(`[]`),
		MetricsJSON:     template.JS(`null`),
		OnlineUsersJSON: template.JS(`null`),
		RawDataSizeStr:  formatBytes(data.Meta.RawDataSize),
		ReportSizeStr:   formatBytes(data.Meta.ReportSize),
	}

	startTime := data.Meta.StartTime
	endTime := data.Meta.EndTime

	if len(data.Tracings) > 0 {
		g.processTracings(td, data.Tracings)
	}
	if len(data.Metrics) > 0 {
		g.processMetrics(td, data.Metrics, startTime, endTime)
	}
	// tracings 为空时（已清洗保存），从 cleaned metrics 重建图表和汇总表
	if td.TotalReqs == 0 && len(data.Metrics) > 0 {
		g.buildChartsFromMetrics(td, data.Metrics, startTime, endTime)
	}
	return td
}

func (g *EChartsHTMLGenerator) processTracings(td *templateData, records []*report.TracingRecord) {
	td.TotalReqs = 0

	// End 记录用于耗时/错误码统计；Start 记录用于 QPS
	caseOrder := make([]string, 0)
	caseRecords := make(map[string][]*report.TracingRecord)
	caseStartBuckets := make(map[string]map[int64]int) // name -> ts -> start count
	for _, r := range records {
		if r == nil || r.Count <= 0 {
			continue
		}
		if r.StartData {
			if _, ok := caseStartBuckets[r.Name]; !ok {
				caseStartBuckets[r.Name] = make(map[int64]int)
			}
			caseStartBuckets[r.Name][r.Timestamp] += r.Count
			continue
		}
		if _, exists := caseRecords[r.Name]; !exists {
			caseOrder = append(caseOrder, r.Name)
		}
		caseRecords[r.Name] = append(caseRecords[r.Name], r)
	}
	if len(caseOrder) == 0 {
		return
	}

	// --- 全局统计 ---
	// TotalReqs 来自 Start 记录，耗时/错误码来自 End 记录
	globalErrors := make(map[string]int)
	var totalMs int64
	var totalEndCount int64
	var totalVarianceSum int64
	var varianceRecordCount int
	for _, startMap := range caseStartBuckets {
		for _, cnt := range startMap {
			td.TotalReqs += cnt
		}
	}
	for _, r := range records {
		if r == nil || r.StartData {
			continue
		}
		totalEndCount += int64(r.Count)
		totalMs += r.TotalDurationMs
		totalVarianceSum += r.Variance
		varianceRecordCount++
		for code, cnt := range r.Code {
			if code == int(report.TracingSuccess) {
				td.SuccessReqs += cnt
			} else {
				td.FailedReqs += cnt
			}
		}
		for msg, cnt := range r.Error {
			globalErrors[msg] += cnt
		}
	}
	if totalEndCount > 0 {
		td.AvgMs = math.Round(float64(totalMs)/float64(totalEndCount)*10) / 10
	}
	if varianceRecordCount > 0 {
		td.VarianceMs = float64(totalVarianceSum) / float64(varianceRecordCount)
	}

	// --- 分 Case 统计 ---
	for _, name := range caseOrder {
		recs := caseRecords[name]
		cs := caseStat{Name: name, MinMs: -1}
		// Total 来自 Start 记录
		for _, cnt := range caseStartBuckets[name] {
			cs.Total += cnt
		}
		var csTotal, csVarianceSum int64
		var csEndCount int
		for _, r := range recs {
			csEndCount += r.Count
			csTotal += r.TotalDurationMs
			csVarianceSum += r.Variance
			for code, cnt := range r.Code {
				if code == int(report.TracingSuccess) {
					cs.Success += cnt
				} else {
					cs.Failed += cnt
				}
			}
			if cs.MinMs < 0 || r.MinDurationMs < cs.MinMs {
				cs.MinMs = r.MinDurationMs
			}
			if r.MaxDurationMs > cs.MaxMs {
				cs.MaxMs = r.MaxDurationMs
			}
		}
		if cs.MinMs < 0 {
			cs.MinMs = 0
		}
		if csEndCount > 0 {
			cs.AvgMs = math.Round(float64(csTotal)/float64(csEndCount)*10) / 10
		}
		if len(recs) > 0 {
			cs.VarianceMs = csVarianceSum / int64(len(recs))
		}
		td.CaseStats = append(td.CaseStats, cs)
	}

	// --- 按秒聚合构建图表数据 ---
	type bucket struct {
		qpsCount      int // 来自 Start 记录
		endCount      int // 来自 End 记录，用于均値计算
		success       int
		failed        int
		totalMs       int64
		varianceSumMs int64
		varianceCount int
	}
	allSecs := make(map[int64]bool)
	caseBuckets := make(map[string]map[int64]*bucket)
	for _, name := range caseOrder {
		caseBuckets[name] = make(map[int64]*bucket)
		for _, r := range caseRecords[name] {
			sec := r.Timestamp
			allSecs[sec] = true
			b := caseBuckets[name][sec]
			if b == nil {
				b = &bucket{}
				caseBuckets[name][sec] = b
			}
			b.endCount += r.Count
			b.totalMs += r.TotalDurationMs
			b.varianceSumMs += r.Variance
			b.varianceCount++
			for code, cnt := range r.Code {
				if code == int(report.TracingSuccess) {
					b.success += cnt
				} else {
					b.failed += cnt
				}
			}
		}
	}
	// 用 Start 记录填充 QPS
	for name, startMap := range caseStartBuckets {
		if _, ok := caseBuckets[name]; !ok {
			continue
		}
		for ts, cnt := range startMap {
			allSecs[ts] = true
			b := caseBuckets[name][ts]
			if b == nil {
				b = &bucket{}
				caseBuckets[name][ts] = b
			}
			b.qpsCount += cnt
		}
	}

	secs := make([]int64, 0, len(allSecs))
	for s := range allSecs {
		secs = append(secs, s)
	}
	sort.Slice(secs, func(i, j int) bool { return secs[i] < secs[j] })

	cd := chartData{TimeLabels: make([]string, len(secs))}
	if len(secs) > 0 {
		cd.StartEpoch = secs[0]
	}
	for i, sec := range secs {
		cd.TimeLabels[i] = time.Unix(sec, 0).Format("2006-01-02 15:04:05")
	}

	// 计算每个 Case 的活跃时间范围
	caseMinSec := make(map[string]int64)
	caseMaxSec := make(map[string]int64)
	for _, name := range caseOrder {
		first := true
		for sec := range caseBuckets[name] {
			if first {
				caseMinSec[name] = sec
				caseMaxSec[name] = sec
				first = false
			} else {
				if sec < caseMinSec[name] {
					caseMinSec[name] = sec
				}
				if sec > caseMaxSec[name] {
					caseMaxSec[name] = sec
				}
			}
		}
	}

	for _, name := range caseOrder {
		sd := chartSeriesData{
			Name:       name,
			MinTime:    time.Unix(caseMinSec[name], 0).Format("2006-01-02 15:04:05"),
			MaxTime:    time.Unix(caseMaxSec[name], 0).Format("2006-01-02 15:04:05"),
			QPS:        make([]int, len(secs)),
			Success:    make([]int, len(secs)),
			Failed:     make([]int, len(secs)),
			AvgMs:      make([]float64, len(secs)),
			VarianceMs: make([]int64, len(secs)),
		}
		bm := caseBuckets[name]
		for i, sec := range secs {
			b := bm[sec]
			if b == nil {
				continue
			}
			sd.QPS[i] = b.qpsCount
			sd.Success[i] = b.success
			sd.Failed[i] = b.failed
			if b.endCount > 0 {
				sd.AvgMs[i] = math.Round(float64(b.totalMs)/float64(b.endCount)*10) / 10
				if b.varianceCount > 0 {
					sd.VarianceMs[i] = b.varianceSumMs / int64(b.varianceCount)
				}
			}
		}
		// Case 错误汇总
		caseErrs := make(map[string]int)
		for _, r := range caseRecords[name] {
			for msg, cnt := range r.Error {
				caseErrs[msg] += cnt
			}
		}
		for errLabel, cnt := range caseErrs {
			sd.Errors = append(sd.Errors, errorCodeEntry{Name: errLabel, Value: cnt})
		}
		cd.Series = append(cd.Series, sd)
	}

	cdJSON, _ := json.Marshal(cd)
	td.ChartDataJSON = template.JS(cdJSON)

	// --- 全局错误汇总 ---
	errorCodes := make([]errorCodeEntry, 0, len(globalErrors))
	for label, cnt := range globalErrors {
		errorCodes = append(errorCodes, errorCodeEntry{Name: label, Value: cnt})
	}
	ecJSON, _ := json.Marshal(errorCodes)
	td.ErrorCodesJSON = template.JS(ecJSON)
}

// seriesKey 构建唯一键：name + 排序后的 label pairs
func seriesKey(s *report.MetricsSeries) string {
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, 1+len(keys))
	parts = append(parts, s.Name)
	for _, k := range keys {
		parts = append(parts, k+"="+s.Labels[k])
	}
	return strings.Join(parts, "\x00")
}

// mergeMetricSeries 合并相同 name+labels 的多条系列（来自多次 RPush 刷新），结果按时间戳排序。
func mergeMetricSeries(series []*report.MetricsSeries) []*report.MetricsSeries {
	type entry struct {
		s   *report.MetricsSeries
		pts []report.MetricsPoint
	}
	merged := make(map[string]*entry)
	order := make([]string, 0, len(series))
	for _, s := range series {
		key := seriesKey(s)
		if e, ok := merged[key]; ok {
			e.pts = append(e.pts, s.Points...)
		} else {
			pts := make([]report.MetricsPoint, len(s.Points))
			copy(pts, s.Points)
			merged[key] = &entry{s: s, pts: pts}
			order = append(order, key)
		}
	}
	result := make([]*report.MetricsSeries, 0, len(order))
	for _, key := range order {
		e := merged[key]
		sort.Slice(e.pts, func(i, j int) bool {
			return e.pts[i].Timestamp.Before(e.pts[j].Timestamp)
		})
		result = append(result, &report.MetricsSeries{
			Name:   e.s.Name,
			Labels: e.s.Labels,
			Points: e.pts,
		})
	}
	return result
}

func (g *EChartsHTMLGenerator) processMetrics(td *templateData, series []*report.MetricsSeries, startTime, endTime time.Time) {
	// 合并相同 name+labels 的系列（来自多次局部 FlushSnapshots RPush）
	series = mergeMetricSeries(series)
	var onlineUsersSeries, otherSeries []*report.MetricsSeries
	for _, s := range series {
		// 按时间范围过滤数据点
		filtered := filterMetricPoints(s.Points, startTime, endTime)
		if len(filtered) == 0 {
			continue
		}
		sc := &report.MetricsSeries{Name: s.Name, Labels: s.Labels, Points: filtered}
		if s.Name == "online_users" {
			onlineUsersSeries = append(onlineUsersSeries, sc)
		} else {
			otherSeries = append(otherSeries, sc)
		}
	}
	g.processOnlineUsers(td, onlineUsersSeries)
	// 其他指标：按 case+name 排序，构建 JSON
	sort.Slice(otherSeries, func(i, j int) bool {
		if otherSeries[i].Labels["case"] != otherSeries[j].Labels["case"] {
			return otherSeries[i].Labels["case"] < otherSeries[j].Labels["case"]
		}
		return otherSeries[i].Name < otherSeries[j].Name
	})
	entries := make([]metricsDataEntry, 0, len(otherSeries))
	for _, s := range otherSeries {
		times := make([]string, len(s.Points))
		values := make([]float64, len(s.Points))
		for i, pt := range s.Points {
			times[i] = pt.Timestamp.Format("2006-01-02 15:04:05")
			values[i] = math.Round(pt.Value*100) / 100
		}
		labelsStr := ""
		for k, v := range s.Labels {
			if k == "case" || k == "agent" {
				continue
			}
			if labelsStr != "" {
				labelsStr += ", "
			}
			labelsStr += k + "=" + v
		}
		entries = append(entries, metricsDataEntry{
			Name:       s.Name,
			CaseGroup:  s.Labels["case"],
			AgentGroup: s.Labels["agent"],
			Labels:     labelsStr,
			Times:      times,
			Values:     values,
		})
		td.MetricsSections = append(td.MetricsSections, metricsSection{}) // 只用于判断是否有数据
	}
	if len(entries) > 0 {
		mj, _ := json.Marshal(entries)
		td.MetricsJSON = template.JS(mj)
	}
}

// filterMetricPoints 返回 [startTime, endTime] 内的数据点；零值时间表示不限制该端。
// start 截断到秒精度，避免纳秒级 StartTime 过滤掉恰好写在整秒的指标点。
func filterMetricPoints(points []report.MetricsPoint, start, end time.Time) []report.MetricsPoint {
	if start.IsZero() && end.IsZero() {
		return points
	}
	startSec := start.Truncate(time.Second) // 指标时间点均截断到整秒，需同精度比较
	result := make([]report.MetricsPoint, 0, len(points))
	for _, p := range points {
		if !startSec.IsZero() && p.Timestamp.Before(startSec) {
			continue
		}
		if !end.IsZero() && p.Timestamp.After(end) {
			continue
		}
		result = append(result, p)
	}
	return result
}

// buildChartsFromMetrics 当 tracings 为空时，从 cleaned tracing metrics重建图表和汇总表。
// 依赖 CleanTracingsToMetrics 生成的 {case}_qps / _success_qps / _failed_qps /
// _avg_ms / _variance_ms 系列。
func (g *EChartsHTMLGenerator) buildChartsFromMetrics(td *templateData, series []*report.MetricsSeries, startTime, endTime time.Time) {
	type caseTimeSeries struct {
		qps        map[string]float64
		successQ   map[string]float64
		failedQ    map[string]float64
		avgMs      map[string]float64
		varianceMs map[string]float64
		minMs      map[string]float64
		maxMs      map[string]float64
		minTime    string
		maxTime    string
	}
	newCTS := func() *caseTimeSeries {
		return &caseTimeSeries{
			qps: make(map[string]float64), successQ: make(map[string]float64),
			failedQ: make(map[string]float64), avgMs: make(map[string]float64),
			varianceMs: make(map[string]float64),
			minMs:      make(map[string]float64), maxMs: make(map[string]float64),
		}
	}

	knownSuffixes := []string{"_qps", "_success_qps", "_failed_qps", "_avg_ms", "_variance_ms", "_min_ms", "_max_ms", "_success_rate"}

	caseOrder := make([]string, 0)
	caseMap := make(map[string]*caseTimeSeries)
	allSecsSet := make(map[string]bool)

	for _, s := range series {
		caseName := s.Labels["case"]
		if caseName == "" {
			continue
		}
		// 确认是 cleaned tracing 指标
		var matchedSuffix string
		for _, sfx := range knownSuffixes {
			if s.Name == caseName+sfx {
				matchedSuffix = sfx
				break
			}
		}
		if matchedSuffix == "" {
			continue
		}
		if _, ok := caseMap[caseName]; !ok {
			caseOrder = append(caseOrder, caseName)
			caseMap[caseName] = newCTS()
		}
		cts := caseMap[caseName]
		for _, pt := range filterMetricPoints(s.Points, startTime, endTime) {
			t := pt.Timestamp.Format("2006-01-02 15:04:05")
			allSecsSet[t] = true
			if cts.minTime == "" || t < cts.minTime {
				cts.minTime = t
			}
			if cts.maxTime == "" || t > cts.maxTime {
				cts.maxTime = t
			}
			switch matchedSuffix {
			case "_qps":
				cts.qps[t] = pt.Value
			case "_success_qps":
				cts.successQ[t] = pt.Value
			case "_failed_qps":
				cts.failedQ[t] = pt.Value
			case "_avg_ms":
				cts.avgMs[t] = pt.Value
			case "_variance_ms":
				cts.varianceMs[t] = pt.Value
			case "_min_ms":
				cts.minMs[t] = pt.Value
			case "_max_ms":
				cts.maxMs[t] = pt.Value
			}
		}
	}
	if len(caseOrder) == 0 {
		return
	}

	// 构建时间标签序列
	timeLabels := make([]string, 0, len(allSecsSet))
	for t := range allSecsSet {
		timeLabels = append(timeLabels, t)
	}
	sort.Strings(timeLabels)
	timeIdx := make(map[string]int, len(timeLabels))
	for i, t := range timeLabels {
		timeIdx[t] = i
	}

	n := len(timeLabels)
	cd := chartData{TimeLabels: timeLabels}
	if len(timeLabels) > 0 {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", timeLabels[0], time.Local); err == nil {
			cd.StartEpoch = t.Unix()
		}
	}

	for _, caseName := range caseOrder {
		cts := caseMap[caseName]
		sd := chartSeriesData{
			Name:       caseName,
			MinTime:    cts.minTime,
			MaxTime:    cts.maxTime,
			QPS:        make([]int, n),
			Success:    make([]int, n),
			Failed:     make([]int, n),
			AvgMs:      make([]float64, n),
			VarianceMs: make([]int64, n),
		}
		var totalReqs, totalSuccess, totalFailed int
		var sumAvg float64
		var sumVariance int64
		var cntP int
		for t, i := range timeIdx {
			q := int(math.Round(cts.qps[t]))
			succ := int(math.Round(cts.successQ[t]))
			fail := int(math.Round(cts.failedQ[t]))
			sd.QPS[i] = q
			sd.Success[i] = succ
			sd.Failed[i] = fail
			sd.AvgMs[i] = math.Round(cts.avgMs[t]*10) / 10
			sd.VarianceMs[i] = int64(math.Round(cts.varianceMs[t]))
			totalReqs += q
			totalSuccess += succ
			totalFailed += fail
			if q > 0 {
				sumAvg += cts.avgMs[t] * float64(q)
				sumVariance += int64(math.Round(cts.varianceMs[t]))
				cntP++
			}
		}
		cd.Series = append(cd.Series, sd)

		// 构建汇总表行（基于每秒指标近似计算）
		cs := caseStat{Name: caseName, Total: totalReqs, Success: totalSuccess, Failed: totalFailed, MinMs: -1}
		if cntP > 0 {
			cs.AvgMs = math.Round(sumAvg/float64(totalReqs)*10) / 10
			cs.VarianceMs = sumVariance / int64(cntP)
		}
		for _, v := range cts.minMs {
			if v > 0 && (cs.MinMs < 0 || int64(v) < cs.MinMs) {
				cs.MinMs = int64(v)
			}
		}
		if cs.MinMs < 0 {
			cs.MinMs = 0
		}
		for _, v := range cts.maxMs {
			if int64(v) > cs.MaxMs {
				cs.MaxMs = int64(v)
			}
		}
		td.CaseStats = append(td.CaseStats, cs)
		td.TotalReqs += totalReqs
		td.SuccessReqs += totalSuccess
		td.FailedReqs += totalFailed
	}

	// 全局延迟：取各 case 加权平均
	if td.TotalReqs > 0 {
		var wAvg, wVariance float64
		for _, cs := range td.CaseStats {
			w := float64(cs.Total)
			wAvg += cs.AvgMs * w
			wVariance += float64(cs.VarianceMs) * w
		}
		total := float64(td.TotalReqs)
		td.AvgMs = math.Round(wAvg/total*10) / 10
		td.VarianceMs = math.Round(wVariance/total*10) / 10
	}

	cdJSON, _ := json.Marshal(cd)
	td.ChartDataJSON = template.JS(cdJSON)
}

// processOnlineUsers 将全部 online_users 系列合并成一张多系列折线图的 JSON。
// 每个 Agent 一条线（按 agent 标签区分，同一 Agent 多批数据合并为一条），并附加一条汇总的 Total 线。
func (g *EChartsHTMLGenerator) processOnlineUsers(td *templateData, series []*report.MetricsSeries) {
	if len(series) == 0 {
		return
	}

	// 按 agent 名称聚合：同一 Agent 的多条系列合并，相同时间戳覆盖取最新值
	type agentPoints struct {
		name   string
		points map[string]float64 // "2006-01-02 15:04:05" -> value
	}
	agentOrder := make([]string, 0)
	agentMap := make(map[string]*agentPoints)

	for _, s := range series {
		name := "online_users"
		if v, ok := s.Labels["agent"]; ok && v != "" {
			name = v
		}
		ap, exists := agentMap[name]
		if !exists {
			ap = &agentPoints{name: name, points: make(map[string]float64)}
			agentOrder = append(agentOrder, name)
			agentMap[name] = ap
		}
		for _, pt := range s.Points {
			t := pt.Timestamp.Format("2006-01-02 15:04:05")
			ap.points[t] = math.Round(pt.Value*100) / 100
		}
	}

	// 收集全部时间点
	timeSet := make(map[string]bool)
	for _, ap := range agentMap {
		for t := range ap.points {
			timeSet[t] = true
		}
	}
	timeLabelsSorted := make([]string, 0, len(timeSet))
	for t := range timeSet {
		timeLabelsSorted = append(timeLabelsSorted, t)
	}
	sort.Strings(timeLabelsSorted)

	type ouEntry struct {
		Name   string    `json:"name"`
		Values []float64 `json:"values"`
	}
	type ouChartData struct {
		TimeLabels []string  `json:"timeLabels"`
		Series     []ouEntry `json:"series"`
	}
	cd := ouChartData{TimeLabels: timeLabelsSorted}
	total := make([]float64, len(timeLabelsSorted))

	for _, name := range agentOrder {
		ap := agentMap[name]
		values := make([]float64, len(timeLabelsSorted))
		for i, t := range timeLabelsSorted {
			if v, ok := ap.points[t]; ok {
				values[i] = v
			}
		}
		cd.Series = append(cd.Series, ouEntry{Name: name, Values: values})
		for i, v := range values {
			total[i] += v
		}
	}

	// 多于一个 Agent 时才显示 Total
	if len(agentOrder) > 1 {
		rounded := make([]float64, len(total))
		for i, v := range total {
			rounded[i] = math.Round(v*100) / 100
		}
		cd.Series = append(cd.Series, ouEntry{Name: "Total", Values: rounded})
	}
	j, _ := json.Marshal(cd)
	td.OnlineUsersJSON = template.JS(j)
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(p)/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

var _ report.HTMLGenerator = (*EChartsHTMLGenerator)(nil)

const htmlTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>{{.Title}} - 压测报告</title>
<script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",sans-serif;background:#f0f2f5;color:#333}
.hdr{background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);color:#fff;padding:28px 40px}
.hdr h1{font-size:22px;margin-bottom:6px}
.hdr .meta{font-size:13px;opacity:.85}
.hdr .actions{margin-top:8px}
.hdr .actions button{padding:6px 16px;background:rgba(255,255,255,0.2);color:#fff;border:1px solid rgba(255,255,255,0.4);border-radius:4px;cursor:pointer;font-size:12px;margin-right:8px}
.hdr .actions button:hover{background:rgba(255,255,255,0.35)}
.wrap{max-width:1400px;margin:0 auto;padding:24px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:14px;margin-bottom:24px}
.cd{background:#fff;border-radius:8px;padding:14px 18px;box-shadow:0 1px 3px rgba(0,0,0,.08);text-align:center}
.cd .lb{font-size:11px;color:#999;text-transform:uppercase;letter-spacing:.5px;margin-bottom:2px}
.cd .vl{font-size:26px;font-weight:700}
.cd .vl.ok{color:#52c41a}.cd .vl.fl{color:#f5222d}.cd .vl.inf{color:#1890ff}
.bx{background:#fff;border-radius:8px;padding:18px;margin-bottom:18px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.bx h3{font-size:14px;color:#555;margin-bottom:10px}
.ct{width:100%;height:400px}
table.st{width:100%;border-collapse:collapse;font-size:13px}
table.st th,table.st td{padding:9px 12px;text-align:right;border-bottom:1px solid #f0f0f0}
table.st th{background:#fafafa;font-weight:600;color:#666}
table.st th:first-child,table.st td:first-child{text-align:left}
table.st tr:hover{background:#f5f5ff}
.stit{font-size:16px;font-weight:600;color:#333;margin:24px 0 14px;padding-left:10px;border-left:3px solid #667eea}
.tr-bar{display:flex;flex-wrap:wrap;align-items:center;gap:10px;padding:10px 18px}
.tr-bar label{font-weight:600;font-size:13px;color:#555}
.tr-bar input[type="text"]{padding:6px 10px;border:1px solid #d9d9d9;border-radius:4px;font-size:12px;width:175px;font-family:monospace;cursor:pointer;background:#fff}
.tr-bar .btn{padding:6px 16px;border:none;border-radius:4px;font-size:13px;cursor:pointer}
.tr-bar .btn-primary{background:#667eea;color:#fff}
.tr-bar .btn-primary:hover{background:#5a6fd6}
.tr-bar .btn-danger{background:#f5222d;color:#fff}
.tr-bar .btn-danger:hover{background:#d91a25}
.tr-bar .hint{color:#999;font-size:11px}
#floatReset{display:none;position:fixed;top:16px;right:24px;z-index:9999;padding:8px 20px;
  background:#f5222d;color:#fff;border:none;border-radius:6px;font-size:13px;cursor:pointer;
  box-shadow:0 2px 8px rgba(245,34,45,0.3);transition:opacity .2s}
#floatReset:hover{background:#d91a25}
.tp-overlay{position:fixed;inset:0;z-index:9998}
.tp-popup{position:fixed;z-index:9999;background:#fff;border-radius:12px;box-shadow:0 8px 30px rgba(0,0,0,.18);padding:16px 12px;min-width:260px;width:auto}
.tp-hdr{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
.tp-title{font-size:14px;color:#333;font-weight:600}
.tp-cols-tp{display:flex;justify-content:center;gap:6px}
.tp-col-wrap{flex:1}
.tp-col-label{text-align:center;font-size:11px;color:#999;margin-bottom:4px;font-weight:600}
.tp-col{height:216px;overflow-y:auto;border:1px solid #eee;border-radius:8px;background:#fafafa}
.tp-col::-webkit-scrollbar{width:3px}
.tp-col::-webkit-scrollbar-thumb{background:#ddd;border-radius:2px}
.tp-item{height:36px;line-height:36px;text-align:center;cursor:pointer;font-size:15px;font-family:monospace;transition:background .12s,color .12s;border-radius:4px;margin:0 2px}
.tp-item:hover{background:#eef0ff}
.tp-item.sel{background:#667eea;color:#fff;font-weight:600}
.tp-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:12px}
.tp-btn{padding:6px 16px;border:none;border-radius:6px;font-size:13px;cursor:pointer;font-weight:500}
.tp-cancel{background:#f5f5f5;color:#666}
.tp-cancel:hover{background:#e8e8e8}
.tp-ok{background:#667eea;color:#fff}
.tp-ok:hover{background:#5a6fd6}
</style>
</head>
<body>
<button id="floatReset">恢复全量</button>
<div class="hdr">
  <h1>{{.Title}}</h1>
  <div class="meta">Report ID: {{.ReportID}} &nbsp;|&nbsp; {{.StartTime}} — {{.EndTime}} &nbsp;|&nbsp; Generated: {{.CreatedAt}}</div>
  <div class="actions"><button onclick="downloadHTML()">&#x2B07; 下载报告</button></div>
</div>
<div class="wrap">
  <div class="cards">
    <div class="cd"><div class="lb">总请求</div><div class="vl inf">{{.TotalReqs}}</div></div>
    <div class="cd"><div class="lb">成功</div><div class="vl ok">{{.SuccessReqs}}</div></div>
    <div class="cd"><div class="lb">失败</div><div class="vl fl">{{.FailedReqs}}</div></div>{{if .RawDataSizeStr}}
    <div class="cd"><div class="lb">原始数据</div><div class="vl" style="font-size:18px">{{.RawDataSizeStr}}</div></div>{{end}}{{if .ReportSizeStr}}
    <div class="cd"><div class="lb">报告大小</div><div class="vl" style="font-size:18px">{{.ReportSizeStr}}</div></div>{{end}}
  </div>

  {{if .CaseStats}}
  <div class="stit">分 Case 统计</div>
  <div class="bx">
    <table class="st">
      <thead><tr><th>Case</th><th>Total</th><th>Success</th><th>Failed</th><th>Avg(ms)</th><th>Var(ms²)</th><th>Min(ms)</th><th>Max(ms)</th></tr></thead>
      <tbody>{{range .CaseStats}}
      <tr><td>{{.Name}}</td><td>{{.Total}}</td><td style="color:#52c41a">{{.Success}}</td><td style="color:#f5222d">{{.Failed}}</td><td>{{printf "%.1f" .AvgMs}}</td><td>{{.VarianceMs}}</td><td>{{.MinMs}}</td><td>{{.MaxMs}}</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  {{end}}

  <div class="bx tr-bar" id="trBar">
    <label>时间范围：</label>
    <input id="trFrom" type="text" readonly placeholder="开始时间">
    <span style="color:#999">—</span>
    <input id="trTo" type="text" readonly placeholder="结束时间">
    <button id="trApply" class="btn btn-primary">应用</button>
    <button id="trReset" class="btn btn-danger" style="display:none">恢复全量</button>
    <span class="hint">可直接在图表上拖动框选时间范围</span>
    <button id="modeToggle" class="btn" style="background:#52c41a;color:#fff" onclick="toggleTimeMode()">经过时间</button>
  </div>

  <div class="bx" style="display:flex;align-items:center;gap:12px;padding:10px 18px">
    <label style="font-weight:600;font-size:13px;color:#555">选择 Case：</label>
    <select id="caseFilter" style="padding:6px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:13px;min-width:200px;cursor:pointer">
      <option value="">全部 Case</option>
    </select>
  </div>

  {{if .OnlineUsersJSON}}
  <div class="stit">在线用户 (Online Users, 点击图例切换显示)</div>
  <div class="bx"><div class="ct" id="c_online_users"></div></div>
  {{end}}

  <div class="stit">QPS 时间曲线 (点击图例切换显示；多 Case 差异大时自动切换对数轴)</div>
  <div class="bx"><div class="ct" id="c_qps"></div></div>

  <div class="stit">延迟方差 (ms²，点击图例切换显示；多 Case 差异大时自动切换对数轴)</div>
  <div class="bx"><div class="ct" id="c_lat"></div></div>

  <div class="stit">成功 / 失败趋势 (多 Case 差异大时自动切换对数轴)</div>
  <div class="bx"><div class="ct" id="c_sf"></div></div>

  <div class="stit">错误码分布</div>
  <div class="bx"><div class="ct" id="c_err" style="height:320px"></div></div>

  {{if .MetricsSections}}
  <div class="stit">指标 (Metrics)</div>
  <div class="bx" style="display:flex;flex-wrap:wrap;align-items:center;gap:12px;padding:10px 18px">
    <label style="font-weight:600;font-size:13px;color:#555">Case：</label>
    <select id="metricCaseFilter" style="padding:6px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:13px;min-width:160px;cursor:pointer">
      <option value="">全部</option>
    </select>
    <label style="font-weight:600;font-size:13px;color:#555">Agent：</label>
    <select id="metricAgentFilter" style="padding:6px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:13px;min-width:160px;cursor:pointer">
      <option value="">全部</option>
    </select>
    <label style="font-weight:600;font-size:13px;color:#555">指标名：</label>
    <select id="metricFilter" style="padding:6px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:13px;min-width:180px;cursor:pointer">
      <option value="">全部</option>
    </select>
  </div>
  <div id="mc"></div>
  {{end}}
</div>

<script>
(function(){
// ===== Data =====
var D={{.ChartDataJSON}};
var EC={{.ErrorCodesJSON}};
var OU={{.OnlineUsersJSON}};
var M={{.MetricsJSON}};
if(!M)M=[];
var C=['#5470c6','#91cc75','#fac858','#ee6666','#73c0de','#3ba272','#fc8452','#9a60b4','#ea7ccc'];
var charts={};
var legendState={};
var maxPts=1000;
var timeMode='wall';
var reportStartEpoch=D.startEpoch||0;
function pd(n){return n<10?'0'+n:''+n;}
function parseLabel(dt){var p=dt.split(' ');var d=p[0].split('-');var t=p[1].split(':');return new Date(+d[0],+d[1]-1,+d[2],+t[0],+t[1],+t[2]);}
var isCrossDay=(function(){var tl=D.timeLabels;if(!tl||tl.length<2)return false;return tl[0].slice(0,10)!==tl[tl.length-1].slice(0,10);})();
function wallLabel(dt){if(!dt||dt.indexOf(' ')<0)return dt;var p=dt.split(' ');return isCrossDay?(p[0].slice(5)+' '+p[1]):p[1];}
function elapsedLabel(dt){if(!dt||!reportStartEpoch)return wallLabel(dt);var ms=parseLabel(dt)-reportStartEpoch*1000;if(ms<0)ms=0;var tot=Math.round(ms/1000),s=tot%60,m=Math.floor(tot/60)%60,h=Math.floor(tot/3600);return '+'+pd(h)+':'+pd(m)+':'+pd(s);}
function displayLabels(tl){if(!tl||!tl.length)return tl;return tl.map(timeMode==='elapsed'?elapsedLabel:wallLabel);}
window.toggleTimeMode=function(){timeMode=(timeMode==='wall')?'elapsed':'wall';var btn=document.getElementById('modeToggle');if(btn)btn.textContent=timeMode==='wall'?'\u7ecf\u8fc7\u65f6\u95f4':'\u5899\u4e0a\u65f6\u95f4';renderAll();};

// ===== Download =====
window.downloadHTML=function(){
  var html='<!DOCTYPE html>\n'+document.documentElement.outerHTML;
  var blob=new Blob([html],{type:'text/html;charset=utf-8'});
  var a=document.createElement('a');
  a.href=URL.createObjectURL(blob);
  a.download=(document.title||'report')+'.html';
  document.body.appendChild(a);a.click();document.body.removeChild(a);
  URL.revokeObjectURL(a.href);
};

// ===== Time Range State =====
var globalFrom=null,globalTo=null;
var hasTimeFilter=false;

// ----- URL params -----
function readUrlParams(){
  try{
    var p=new URLSearchParams(window.location.search);
    var f=p.get('from'),t=p.get('to');
    if(f)globalFrom=f;
    if(t)globalTo=t;
  }catch(e){}
}
function writeUrlParams(){
  try{
    var p=new URLSearchParams(window.location.search);
    if(globalFrom)p.set('from',globalFrom);else p.delete('from');
    if(globalTo)p.set('to',globalTo);else p.delete('to');
    var qs=p.toString();
    var url=window.location.pathname+(qs?'?'+qs:'');
    window.history.replaceState(null,'',url);
  }catch(e){}
}

// ===== Downsample helpers (JS side) =====
function dsArray(arr,bs){
  var out=[];
  for(var i=0;i<arr.length;i+=bs){
    var e=Math.min(i+bs,arr.length),sum=0,cnt=0;
    for(var j=i;j<e;j++){
      if(arr[j]!==null&&arr[j]!==undefined){sum+=arr[j];cnt++;}
    }
    out.push(cnt>0?sum/cnt:null);
  }
  return out;
}
function dsLabels(labels,bs){
  var out=[];
  for(var i=0;i<labels.length;i+=bs){
    var e=Math.min(i+bs,labels.length);
    out.push(labels[e-1]);
  }
  return out;
}
function dsChartData(cd){
  var n=cd.timeLabels.length;
  if(n<=maxPts)return cd;
  var bs=Math.ceil(n/maxPts);
  var newTl=dsLabels(cd.timeLabels,bs);
  var newSeries=cd.series.map(function(s){
    return {name:s.name,minTime:s.minTime,maxTime:s.maxTime,
      qps:dsArray(s.qps,bs),success:dsArray(s.success,bs),
      failed:dsArray(s.failed,bs),avgMs:dsArray(s.avgMs,bs),
      varianceMs:dsArray(s.varianceMs,bs),errors:s.errors};
  });
  return {timeLabels:newTl,series:newSeries};
}
function dsOU(cd){
  if(!cd||!cd.series)return cd;
  var n=cd.timeLabels.length;
  if(n<=maxPts)return cd;
  var bs=Math.ceil(n/maxPts);
  var newTl=dsLabels(cd.timeLabels,bs);
  return {timeLabels:newTl,series:cd.series.map(function(s){
    return {name:s.name,values:dsArray(s.values,bs)};
  })};
}
function dsMetric(m){
  if(!m||m.times.length<=maxPts)return m;
  var bs=Math.ceil(m.times.length/maxPts);
  var newT=dsLabels(m.times,bs);
  return {name:m.name,caseGroup:m.caseGroup,agentGroup:m.agentGroup,labels:m.labels,
    times:newT,values:dsArray(m.values,bs)};
}

// ----- Filter helpers -----
function filterChartData(src,from,to){
  if(!from&&!to)return dsChartData(src);
  var idxs=[];
  src.timeLabels.forEach(function(t,i){if((!from||t>=from)&&(!to||t<=to))idxs.push(i);});
  if(idxs.length===0)return {timeLabels:[],series:src.series.map(function(s){return {name:s.name,minTime:s.minTime,maxTime:s.maxTime,qps:[],success:[],failed:[],avgMs:[],varianceMs:[],errors:s.errors};})};
  if(idxs.length===src.timeLabels.length)return dsChartData(src);
  var filtered={
    timeLabels:idxs.map(function(i){return src.timeLabels[i];}),
    series:src.series.map(function(s){return {
      name:s.name,minTime:s.minTime,maxTime:s.maxTime,
      qps:idxs.map(function(i){return s.qps[i];}),
      success:idxs.map(function(i){return s.success[i];}),
      failed:idxs.map(function(i){return s.failed[i];}),
      avgMs:idxs.map(function(i){return s.avgMs[i];}),
      varianceMs:idxs.map(function(i){return s.varianceMs[i];}),
      errors:s.errors};})
  };
  return dsChartData(filtered);
}
function filterOU(src,from,to){
  if(!src||!src.series)return src;
  if(!from&&!to)return dsOU(src);
  var idxs=[];
  src.timeLabels.forEach(function(t,i){if((!from||t>=from)&&(!to||t<=to))idxs.push(i);});
  if(idxs.length===src.timeLabels.length)return dsOU(src);
  var filtered={
    timeLabels:idxs.map(function(i){return src.timeLabels[i];}),
    series:src.series.map(function(s){return {name:s.name,values:idxs.map(function(i){return s.values[i];})};})
  };
  return dsOU(filtered);
}
function filterMetrics(src,from,to){
  if(!src)return src;
  return src.map(function(m){
    var fm=m;
    if(from||to){
      var idxs=[];
      m.times.forEach(function(t,i){if((!from||t>=from)&&(!to||t<=to))idxs.push(i);});
      fm={name:m.name,caseGroup:m.caseGroup,agentGroup:m.agentGroup,labels:m.labels,
        times:idxs.map(function(i){return m.times[i];}),values:idxs.map(function(i){return m.values[i];})};
    }
    return dsMetric(fm);
  });
}

// Mask: series with no non-zero data -> all null (issue #6: no zero points when no data).
// Series with data -> null outside active range.
function mask(data){
  var first=-1,last=-1;
  for(var i=0;i<data.length;i++){
    if(data[i]!==null&&data[i]!==0&&data[i]!==undefined){
      if(first<0)first=i;
      last=i;
    }
  }
  if(first<0)return data.map(function(){return null;});
  if(first===0&&last===data.length-1)return data;
  return data.map(function(v,i){return(i<first||i>last)?null:v;});
}

// Count non-null non-zero values
function nnz(d){var c=0;for(var i=0;i<d.length;i++)if(d[i]!==null&&d[i]!==0&&d[i]!==undefined)c++;return c;}

// Check if multi-series data needs log scale
function needLogAxis(seriesArr){
  var maxAvg=0,minAvg=Infinity,valid=0;
  seriesArr.forEach(function(data){
    var sum=0,cnt=0;
    data.forEach(function(v){if(v!==null&&v>0){sum+=v;cnt++;}});
    if(cnt>0){valid++;var a=sum/cnt;if(a>maxAvg)maxAvg=a;if(a<minAvg)minAvg=a;}
  });
  return valid>=2&&minAvg>0&&maxAvg/minAvg>20;
}

// Build series option with adaptive symbol size
function seriesOpt(name,data,color,extra){
  var nn=nnz(data);
  var symSize=nn<=3?14:nn<=10?10:nn<=30?6:3;
  var o={name:name,type:'line',smooth:nn>5,symbol:'circle',symbolSize:symSize,
    showSymbol:true,data:data,itemStyle:{color:color},emphasis:{focus:'series'},connectNulls:false};
  if(extra)for(var k in extra)o[k]=extra[k];
  return o;
}

// ----- Chart helpers -----
function saveLegend(id){
  var c=charts[id];
  if(c){
    try{var opt=c.getOption();if(opt&&opt.legend&&opt.legend[0]&&opt.legend[0].selected)legendState[id]=opt.legend[0].selected;}catch(e){}
  }
}
function mk(id,opt){
  var d=document.getElementById(id);if(!d)return null;
  saveLegend(id);
  if(charts[id]){charts[id].dispose();}
  if(legendState[id]&&opt.legend){opt.legend.selected=legendState[id];}
  var c=echarts.init(d);c.setOption(opt);
  window.addEventListener('resize',function(){c.resize();});
  charts[id]=c;
  return c;
}

function addBrush(chart,timeLabels){
  if(!chart||!timeLabels||!timeLabels.length)return;
  chart.setOption({toolbox:{show:false},brush:{xAxisIndex:0,brushType:'lineX',brushMode:'single',throttleType:'debounce',throttleDelay:300},xAxis:{boundaryGap:true,axisLabel:{showMaxLabel:true,showMinLabel:true}}});
  chart.dispatchAction({type:'takeGlobalCursor',key:'brush',brushOption:{brushType:'lineX',brushMode:'single'}});
  var lastAreas=null;
  chart.on('brush',function(p){lastAreas=p.areas;});
  chart.on('brushEnd',function(){
    if(lastAreas&&lastAreas.length>0){
      var a=lastAreas[0];
      if(a.coordRange){
        var rawS=Math.round(a.coordRange[0]);
        var rawE=Math.round(a.coordRange[1]);
        var s=Math.max(0,rawS);
        var e=Math.min(timeLabels.length-1,rawE);
        // Clamp: if drag extends past left edge use first label; past right use last
        var fromT=(rawS<0)?timeLabels[0]:timeLabels[s];
        var toT=(rawE>=timeLabels.length)?timeLabels[timeLabels.length-1]:timeLabels[e];
        if(fromT<=toT)setTimeRange(fromT,toT);
      }
      lastAreas=null;
    }
  });
}

// ----- Float reset button & IntersectionObserver -----
function initFloatReset(){
  var trBar=document.getElementById('trBar');
  var fb=document.getElementById('floatReset');
  if(!trBar||!fb)return;
  var isBarVisible=true;
  function updateFloat(){
    fb.style.display=(hasTimeFilter&&!isBarVisible)?'block':'none';
  }
  if(window.IntersectionObserver){
    new IntersectionObserver(function(entries){
      isBarVisible=entries[0].isIntersecting;
      updateFloat();
    },{threshold:0}).observe(trBar);
  }else{
    isBarVisible=false;
  }
  fb.addEventListener('click',function(){setTimeRange(null,null);});
  window._updateFloat=updateFloat;
}

// ----- Time Range UI -----
function setTimeRange(from,to){
  globalFrom=from||null;
  globalTo=to||null;
  hasTimeFilter=!!(globalFrom||globalTo);
  var fi=document.getElementById('trFrom'),ti=document.getElementById('trTo');
  if(fi)fi.value=globalFrom||'';
  if(ti)ti.value=globalTo||'';
  writeUrlParams();
  var rb=document.getElementById('trReset');
  if(rb)rb.style.display=hasTimeFilter?'inline-block':'none';
  if(window._updateFloat)window._updateFloat();
  renderAll();
}

// Compute the active data time range for selected case(s)
function caseDataRange(caseName){
  if(!caseName)return {from:null,to:null};
  var s=D.series.find(function(x){return x.name===caseName;});
  if(!s)return {from:null,to:null};
  // Find first/last nonzero in original full data
  var first=-1,last=-1;
  for(var i=0;i<s.qps.length;i++){
    if(s.qps[i]>0||s.success[i]>0||s.failed[i]>0){
      if(first<0)first=i;
      last=i;
    }
  }
  if(first<0)return {from:null,to:null};
  return {from:D.timeLabels[first],to:D.timeLabels[last]};
}

// ----- Main render -----
function renderAll(){
  var cs=document.getElementById('caseFilter');
  var caseName=cs?cs.value:'';
  var cd=filterChartData(D,globalFrom,globalTo);
  renderCharts(cd,caseName);
  renderOnlineUsers();
  renderMetrics();
}

// ----- Case filter -----
var sel=document.getElementById('caseFilter');
if(sel){
  D.series.forEach(function(s){
    var o=document.createElement('option');o.value=s.name;o.textContent=s.name;sel.appendChild(o);
  });
  sel.addEventListener('change',function(){
    // Issue #5: auto-zoom to case time range
    var cn=sel.value;
    if(cn){
      var r=caseDataRange(cn);
      if(r.from&&r.to){
        globalFrom=r.from;globalTo=r.to;
        hasTimeFilter=true;
        var fi=document.getElementById('trFrom'),ti=document.getElementById('trTo');
        if(fi)fi.value=globalFrom;
        if(ti)ti.value=globalTo;
        writeUrlParams();
        var rb=document.getElementById('trReset');
        if(rb)rb.style.display='inline-block';
        if(window._updateFloat)window._updateFloat();
      }
    } else {
      // Back to all: clear time filter
      globalFrom=null;globalTo=null;
      hasTimeFilter=false;
      var fi=document.getElementById('trFrom'),ti=document.getElementById('trTo');
      if(fi)fi.value='';
      if(ti)ti.value='';
      writeUrlParams();
      var rb=document.getElementById('trReset');
      if(rb)rb.style.display='none';
      if(window._updateFloat)window._updateFloat();
    }
    renderAll();
  });
}

function renderCharts(cd,caseName){
  var tl=cd.timeLabels;var xtl=displayLabels(tl);
  var fs=caseName?cd.series.filter(function(s){return s.name===caseName;}):cd.series;

  // ---- QPS ----
  var qS=[],qData=[];
  fs.forEach(function(s){
    var gi=D.series.findIndex(function(x){return x.name===s.name;});if(gi<0)gi=0;
    var d=mask(s.qps);
    qData.push(d);
    qS.push(seriesOpt(s.name,d,C[gi%C.length]));
  });
  var qLog=needLogAxis(qData);
  var c1=mk('c_qps',{
    tooltip:{trigger:'axis'},
    legend:{data:fs.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:40,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:xtl,boundaryGap:false,axisLabel:{rotate:30,fontSize:11}},
    yAxis:qLog?{type:'log',name:'req/s (log)',min:1,logBase:10}:{type:'value',name:'req/s'},
    series:qS
  });
  addBrush(c1,tl);

  // ---- Variance ----
  var lS=[],lData=[];
  fs.forEach(function(s){
    var gi=D.series.findIndex(function(x){return x.name===s.name;});if(gi<0)gi=0;
    var d=mask(s.varianceMs);
    lData.push(d);
    lS.push(seriesOpt(s.name,d,C[gi%C.length],{lineStyle:{width:1.5}}));
  });
  var lLog=needLogAxis(lData);
  var c2=mk('c_lat',{
    tooltip:{trigger:'axis'},
    legend:{data:lS.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:45,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:xtl,boundaryGap:false,axisLabel:{rotate:30,fontSize:11}},
    yAxis:lLog?{type:'log',name:'ms\u00b2 (log)',min:1,logBase:10}:{type:'value',name:'ms\u00b2'},
    series:lS
  });
  addBrush(c2,tl);

  // ---- Success / Failed ----
  var sfS=[],sfData=[];
  fs.forEach(function(s){
    var gi=D.series.findIndex(function(x){return x.name===s.name;});if(gi<0)gi=0;
    var ds=mask(s.success),df=mask(s.failed);
    sfData.push(ds);sfData.push(df);
    sfS.push(seriesOpt(s.name+' \u6210\u529f',ds,C[gi%C.length],{areaStyle:{opacity:.2}}));
    sfS.push(seriesOpt(s.name+' \u5931\u8d25',df,C[(gi+3)%C.length],{lineStyle:{type:'dashed'}}));
  });
  var sfLog=needLogAxis(sfData);
  var c3=mk('c_sf',{
    tooltip:{trigger:'axis'},
    legend:{data:sfS.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:40,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:xtl,boundaryGap:false,axisLabel:{rotate:30,fontSize:11}},
    yAxis:sfLog?{type:'log',name:'(log)',min:1,logBase:10}:{type:'value'},
    series:sfS
  });
  addBrush(c3,tl);

  // ---- Error Pie ----
  var errData;
  if(!caseName){errData=EC;}else{
    errData=[];fs.forEach(function(s){if(s.errors)errData=errData.concat(s.errors);});
  }
  mk('c_err',{
    tooltip:{trigger:'item',formatter:'{b}: {c} ({d}%)'},
    series:[{type:'pie',radius:['30%','55%'],data:errData,label:{formatter:'{b}\n{c} ({d}%)'}}]
  });
}

// ---- Online Users ----
function renderOnlineUsers(){
  if(!OU||!OU.series||OU.series.length===0)return;
  var d=filterOU(OU,globalFrom,globalTo);
  var tl=d.timeLabels;var xtl=displayLabels(tl);
  var ouS=d.series.map(function(s,i){
    return {name:s.name,type:'line',smooth:true,symbol:'circle',symbolSize:3,showSymbol:true,data:s.values,
      itemStyle:{color:C[i%C.length]},emphasis:{focus:'series'}};
  });
  var c=mk('c_online_users',{
    tooltip:{trigger:'axis'},
    legend:{data:d.series.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:40,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:xtl,boundaryGap:false,axisLabel:{rotate:30,fontSize:11}},
    yAxis:{type:'value',name:'users'},
    series:ouS
  });
  addBrush(c,tl);
}

// ---- Metrics ----
(function(){
  var metSel=document.getElementById('metricFilter');
  var caseSel=document.getElementById('metricCaseFilter');
  var agentSel=document.getElementById('metricAgentFilter');
  if(M.length>0){
    var seenN={},seenC={},seenA={};
    M.forEach(function(m){
      if(metSel&&!seenN[m.name]){seenN[m.name]=true;
        var o=document.createElement('option');o.value=m.name;o.textContent=m.name;metSel.appendChild(o);}
      if(caseSel&&m.caseGroup&&!seenC[m.caseGroup]){seenC[m.caseGroup]=true;
        var o=document.createElement('option');o.value=m.caseGroup;o.textContent=m.caseGroup;caseSel.appendChild(o);}
      if(agentSel&&m.agentGroup&&!seenA[m.agentGroup]){seenA[m.agentGroup]=true;
        var o=document.createElement('option');o.value=m.agentGroup;o.textContent=m.agentGroup;agentSel.appendChild(o);}
    });
    function onFilter(){renderMetrics();}
    if(metSel)metSel.addEventListener('change',onFilter);
    if(caseSel)caseSel.addEventListener('change',onFilter);
    if(agentSel)agentSel.addEventListener('change',onFilter);
  }
})();

function renderMetrics(){
  var mc=document.getElementById('mc');if(!mc)return;
  var fn=document.getElementById('metricFilter');fn=fn?fn.value:'';
  var fc=document.getElementById('metricCaseFilter');fc=fc?fc.value:'';
  var fa=document.getElementById('metricAgentFilter');fa=fa?fa.value:'';
  Object.keys(charts).forEach(function(id){if(id.indexOf('c_mx')===0){charts[id].dispose();delete charts[id];}});
  mc.innerHTML='';
  var mf=filterMetrics(M,globalFrom,globalTo);
  var fms=mf.filter(function(m){
    if(fn&&m.name!==fn)return false;
    if(fc&&m.caseGroup!==fc)return false;
    if(fa&&m.agentGroup!==fa)return false;
    return true;
  });
  if(!fms.length){mc.innerHTML='<div style="text-align:center;padding:40px;color:#999">No metrics.</div>';return;}
  var prevGroup='\x00';
  fms.forEach(function(m,i){
    var grpKey=(m.caseGroup||'')+'|'+(m.agentGroup||'');
    if(grpKey!==prevGroup){
      prevGroup=grpKey;
      var parts=[];
      if(m.caseGroup)parts.push('Case: '+m.caseGroup);
      if(m.agentGroup)parts.push('Agent: '+m.agentGroup);
      if(parts.length){
        var hdr=document.createElement('div');hdr.className='stit';
        hdr.style.cssText='font-size:13px;margin:16px 0 10px;border-left-color:#91cc75;color:#3ba272';
        hdr.textContent=parts.join('  /  ');mc.appendChild(hdr);
      }
    }
    var id='c_mx'+i;
    var bx=document.createElement('div');bx.className='bx';
    var h3=document.createElement('h3');
    h3.innerHTML=m.name+(m.labels?' <span style="font-weight:normal;color:#999">('+m.labels+')</span>':'');
    var ct=document.createElement('div');ct.className='ct';ct.id=id;
    bx.appendChild(h3);bx.appendChild(ct);mc.appendChild(bx);
    (function(cid,cm){
      var c=mk(cid,{
        tooltip:{trigger:'axis'},
        grid:{top:20,bottom:35,left:55,right:20},
        xAxis:{type:'category',data:displayLabels(cm.times),boundaryGap:false,axisLabel:{rotate:30,fontSize:11}},
        yAxis:{type:'value'},
        series:[{data:cm.values,type:'line',smooth:true,name:cm.name,showSymbol:true,symbol:'circle',symbolSize:3,areaStyle:{opacity:.12}}]
      });
      addBrush(c,cm.times);
    })(id,m);
  });
}

// ===== Init =====
readUrlParams();
hasTimeFilter=!!(globalFrom||globalTo);
var trFrom=document.getElementById('trFrom'),trTo=document.getElementById('trTo');
var trApply=document.getElementById('trApply'),trReset=document.getElementById('trReset');
if(trFrom&&globalFrom)trFrom.value=globalFrom;
if(trTo&&globalTo)trTo.value=globalTo;
if(trReset)trReset.style.display=hasTimeFilter?'inline-block':'none';
if(trApply)trApply.addEventListener('click',function(){
  setTimeRange(trFrom.value||null,trTo.value||null);
});
if(trReset)trReset.addEventListener('click',function(){setTimeRange(null,null);});
var activeTP=null;
var uniqueDates=(function(){var seen={},a=[];(D.timeLabels||[]).forEach(function(t){var d=t.slice(0,10);if(!seen[d]){seen[d]=true;a.push(d);}});return a;})();
function TimePicker(inp){
var self=this;self.popup=null;self.overlay=null;
self.date=uniqueDates.length?uniqueDates[0]:'';
self.h=0;self.m=0;self.s=0;
self.hCol=null;self.mCol=null;self.sCol=null;self.dCol=null;
function buildCol(items,selIdx,onChange){
  var col=document.createElement('div');col.className='tp-col';
  items.forEach(function(txt,i){
    var d=document.createElement('div');d.className='tp-item'+(i===selIdx?' sel':'');
    d.textContent=txt;
    d.addEventListener('click',function(){
      col.querySelectorAll('.tp-item').forEach(function(el){el.classList.remove('sel');});
      d.classList.add('sel');onChange(i);
    });
    col.appendChild(d);
  });
  return col;
}
function numItems(cnt){var a=[];for(var i=0;i<cnt;i++)a.push(pd(i));return a;}
function scrollTo(col,idx){setTimeout(function(){col.scrollTop=idx*36-col.clientHeight/2+18;},0);}
self.close=function(){
  if(self.popup&&self.popup.parentNode)self.popup.parentNode.removeChild(self.popup);
  if(self.overlay&&self.overlay.parentNode)self.overlay.parentNode.removeChild(self.overlay);
  self.popup=null;self.overlay=null;if(activeTP===self)activeTP=null;
};
self.open=function(){
  if(activeTP&&activeTP!==self)activeTP.close();
  if(self.popup){self.close();return;}
  activeTP=self;
  var val=inp.value;
  var m1=val&&val.match(/^(\d{4}-\d{2}-\d{2})\s+(\d{2}):(\d{2}):(\d{2})$/);
  if(m1){self.date=m1[1];self.h=+m1[2];self.m=+m1[3];self.s=+m1[4];}
  if(!self.date&&uniqueDates.length)self.date=uniqueDates[0];
  var dateIdx=Math.max(0,uniqueDates.indexOf(self.date));
  self.overlay=document.createElement('div');self.overlay.className='tp-overlay';
  self.overlay.addEventListener('click',function(){self.close();});
  document.body.appendChild(self.overlay);
  self.popup=document.createElement('div');self.popup.className='tp-popup';
  self.popup.style.width=isCrossDay?'310px':'265px';
  self.popup.addEventListener('click',function(e){e.stopPropagation();});
  var hdr=document.createElement('div');hdr.className='tp-hdr';
  var title=document.createElement('div');title.className='tp-title';title.textContent='\u9009\u62e9\u65f6\u95f4';
  hdr.appendChild(title);self.popup.appendChild(hdr);
  var cols=document.createElement('div');cols.className='tp-cols-tp';
  function makeWrap(label,col){var wrap=document.createElement('div');wrap.className='tp-col-wrap';var lb=document.createElement('div');lb.className='tp-col-label';lb.textContent=label;wrap.appendChild(lb);wrap.appendChild(col);return wrap;}
  if(isCrossDay){
    var dc=buildCol(uniqueDates.map(function(d){return d.slice(5);}),dateIdx,function(i){self.date=uniqueDates[i];});
    self.dCol=dc;cols.appendChild(makeWrap('\u65e5\u671f',dc));
  }
  var hc=buildCol(numItems(24),self.h,function(i){self.h=i;});self.hCol=hc;
  var mc=buildCol(numItems(60),self.m,function(i){self.m=i;});self.mCol=mc;
  var sc=buildCol(numItems(60),self.s,function(i){self.s=i;});self.sCol=sc;
  cols.appendChild(makeWrap('\u65f6',hc));cols.appendChild(makeWrap('\u5206',mc));cols.appendChild(makeWrap('\u79d2',sc));
  self.popup.appendChild(cols);
  var actions=document.createElement('div');actions.className='tp-actions';
  var cb=document.createElement('button');cb.className='tp-btn tp-cancel';cb.textContent='\u53d6\u6d88';
  cb.addEventListener('click',function(){self.close();});
  var ob=document.createElement('button');ob.className='tp-btn tp-ok';ob.textContent='\u786e\u5b9a';
  ob.addEventListener('click',function(){
    inp.value=self.date+' '+pd(self.h)+':'+pd(self.m)+':'+pd(self.s);
    self.close();
    if(trFrom&&trTo&&trFrom.value&&trTo.value)setTimeRange(trFrom.value,trTo.value);
  });
  actions.appendChild(cb);actions.appendChild(ob);self.popup.appendChild(actions);
  var rect=inp.getBoundingClientRect();
  var popH=300;
  var top=rect.bottom+4;if(top+popH>window.innerHeight)top=rect.top-popH;
  self.popup.style.left=Math.max(8,rect.left)+'px';
  self.popup.style.top=Math.max(8,top)+'px';
  document.body.appendChild(self.popup);
  if(isCrossDay&&self.dCol)scrollTo(self.dCol,dateIdx);
  scrollTo(self.hCol,self.h);scrollTo(self.mCol,self.m);scrollTo(self.sCol,self.s);
};
inp.addEventListener('click',function(e){e.stopPropagation();self.open();});
}
if(trFrom)new TimePicker(trFrom);
if(trTo)new TimePicker(trTo);

initFloatReset();
renderAll();
})();
</script>
</body>
</html>`
