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
	Name    string  `json:"name"`
	Total   int     `json:"total"`
	Success int     `json:"success"`
	Failed  int     `json:"failed"`
	AvgMs   float64 `json:"avgMs"`
	P50Ms   int64   `json:"p50Ms"`
	P90Ms   int64   `json:"p90Ms"`
	P99Ms   int64   `json:"p99Ms"`
	MinMs   int64   `json:"minMs"`
	MaxMs   int64   `json:"maxMs"`
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
	CaseGroup  string    `json:"caseGroup"`
	AgentGroup string    `json:"agentGroup"`
	Labels     string    `json:"labels"`
	Times      []string  `json:"times"`
	Values     []float64 `json:"values"`
}

// chartSeriesData 按秒聚合的单条 case 时间序列
type chartSeriesData struct {
	Name    string           `json:"name"`
	QPS     []int            `json:"qps"`
	Success []int            `json:"success"`
	Failed  []int            `json:"failed"`
	AvgMs   []float64        `json:"avgMs"`
	P50Ms   []int64          `json:"p50Ms"`
	P90Ms   []int64          `json:"p90Ms"`
	P99Ms   []int64          `json:"p99Ms"`
	Errors  []errorCodeEntry `json:"errors"`
}

type chartData struct {
	TimeLabels []string          `json:"timeLabels"`
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
	P50Ms       float64
	P90Ms       float64
	P99Ms       float64
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
	td.TotalReqs = len(records)

	// --- 按 case name 分组（保持出现顺序）---
	caseOrder := make([]string, 0)
	caseRecords := make(map[string][]*report.TracingRecord)
	for _, r := range records {
		if _, exists := caseRecords[r.Name]; !exists {
			caseOrder = append(caseOrder, r.Name)
		}
		caseRecords[r.Name] = append(caseRecords[r.Name], r)
	}

	// --- 全局统计 ---
	allDurations := make([]int64, 0, len(records))
	globalErrorCodes := make(map[string]int)
	var totalMs int64
	for _, r := range records {
		totalMs += r.DurationMs
		allDurations = append(allDurations, r.DurationMs)
		if r.Code == report.TracingSuccess {
			td.SuccessReqs++
		} else {
			td.FailedReqs++
			label := fmt.Sprintf("code_%d", r.Code)
			if msg, ok := r.Extra["error"]; ok && msg != "" {
				label = msg
			}
			globalErrorCodes[label]++
		}
	}
	if td.TotalReqs > 0 {
		td.AvgMs = float64(totalMs) / float64(td.TotalReqs)
		sort.Slice(allDurations, func(i, j int) bool { return allDurations[i] < allDurations[j] })
		td.P50Ms = float64(percentile(allDurations, 50))
		td.P90Ms = float64(percentile(allDurations, 90))
		td.P99Ms = float64(percentile(allDurations, 99))
	}

	// --- 分 Case 统计 ---
	for _, name := range caseOrder {
		recs := caseRecords[name]
		cs := caseStat{Name: name, Total: len(recs)}
		var csTotal int64
		durs := make([]int64, 0, len(recs))
		for _, r := range recs {
			csTotal += r.DurationMs
			durs = append(durs, r.DurationMs)
			if r.Code == report.TracingSuccess {
				cs.Success++
			} else {
				cs.Failed++
			}
		}
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		if len(durs) > 0 {
			cs.AvgMs = math.Round(float64(csTotal)/float64(len(durs))*10) / 10
			cs.P50Ms = percentile(durs, 50)
			cs.P90Ms = percentile(durs, 90)
			cs.P99Ms = percentile(durs, 99)
			cs.MinMs = durs[0]
			cs.MaxMs = durs[len(durs)-1]
		}
		td.CaseStats = append(td.CaseStats, cs)
	}

	// --- 按秒聚合（每个 case 独立 bucket）---
	type bucket struct {
		success int
		failed  int
		totalMs int64
		count   int
		durs    []int64
	}
	allSecs := make(map[int64]bool)
	caseBuckets := make(map[string]map[int64]*bucket)
	for _, name := range caseOrder {
		caseBuckets[name] = make(map[int64]*bucket)
		for _, r := range caseRecords[name] {
			sec := r.EndTime.Unix()
			allSecs[sec] = true
			b := caseBuckets[name][sec]
			if b == nil {
				b = &bucket{}
				caseBuckets[name][sec] = b
			}
			b.count++
			b.totalMs += r.DurationMs
			b.durs = append(b.durs, r.DurationMs)
			if r.Code == report.TracingSuccess {
				b.success++
			} else {
				b.failed++
			}
		}
	}

	secs := make([]int64, 0, len(allSecs))
	for s := range allSecs {
		secs = append(secs, s)
	}
	sort.Slice(secs, func(i, j int) bool { return secs[i] < secs[j] })

	cd := chartData{TimeLabels: make([]string, len(secs))}
	for i, sec := range secs {
		cd.TimeLabels[i] = time.Unix(sec, 0).Format("15:04:05")
	}
	for _, name := range caseOrder {
		sd := chartSeriesData{
			Name:    name,
			QPS:     make([]int, len(secs)),
			Success: make([]int, len(secs)),
			Failed:  make([]int, len(secs)),
			AvgMs:   make([]float64, len(secs)),
			P50Ms:   make([]int64, len(secs)),
			P90Ms:   make([]int64, len(secs)),
			P99Ms:   make([]int64, len(secs)),
		}
		bm := caseBuckets[name]
		for i, sec := range secs {
			b := bm[sec]
			if b == nil {
				continue
			}
			sd.QPS[i] = b.count
			sd.Success[i] = b.success
			sd.Failed[i] = b.failed
			if b.count > 0 {
				sd.AvgMs[i] = math.Round(float64(b.totalMs)/float64(b.count)*10) / 10
			}
			sort.Slice(b.durs, func(a, c int) bool { return b.durs[a] < b.durs[c] })
			sd.P50Ms[i] = percentile(b.durs, 50)
			sd.P90Ms[i] = percentile(b.durs, 90)
			sd.P99Ms[i] = percentile(b.durs, 99)
		}
		// 每个 case 独立的错误码统计（供 case 筛选下拉框过滤饼图）
		caseErrs := make(map[string]int)
		for _, r := range caseRecords[name] {
			if r.Code != report.TracingSuccess {
				label := fmt.Sprintf("code_%d", r.Code)
				if msg, ok := r.Extra["error"]; ok && msg != "" {
					label = msg
				}
				caseErrs[label]++
			}
		}
		for errLabel, cnt := range caseErrs {
			sd.Errors = append(sd.Errors, errorCodeEntry{Name: errLabel, Value: cnt})
		}
		cd.Series = append(cd.Series, sd)
	}

	cdJSON, _ := json.Marshal(cd)
	td.ChartDataJSON = template.JS(cdJSON)

	// --- 错误码 ---
	errorCodes := make([]errorCodeEntry, 0, len(globalErrorCodes))
	for label, cnt := range globalErrorCodes {
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
			times[i] = pt.Timestamp.Format("15:04:05")
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
func filterMetricPoints(points []report.MetricsPoint, start, end time.Time) []report.MetricsPoint {
	if start.IsZero() && end.IsZero() {
		return points
	}
	result := make([]report.MetricsPoint, 0, len(points))
	for _, p := range points {
		if !start.IsZero() && p.Timestamp.Before(start) {
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
// _avg_ms / _p50_ms / _p90_ms / _p99_ms 系列。
func (g *EChartsHTMLGenerator) buildChartsFromMetrics(td *templateData, series []*report.MetricsSeries, startTime, endTime time.Time) {
	type caseTimeSeries struct {
		qps      map[string]float64
		successQ map[string]float64
		failedQ  map[string]float64
		avgMs    map[string]float64
		p50Ms    map[string]float64
		p90Ms    map[string]float64
		p99Ms    map[string]float64
	}
	newCTS := func() *caseTimeSeries {
		return &caseTimeSeries{
			qps: make(map[string]float64), successQ: make(map[string]float64),
			failedQ: make(map[string]float64), avgMs: make(map[string]float64),
			p50Ms: make(map[string]float64), p90Ms: make(map[string]float64),
			p99Ms: make(map[string]float64),
		}
	}

	knownSuffixes := []string{"_qps", "_success_qps", "_failed_qps", "_avg_ms", "_p50_ms", "_p90_ms", "_p99_ms", "_success_rate"}

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
			t := pt.Timestamp.Format("15:04:05")
			allSecsSet[t] = true
			switch matchedSuffix {
			case "_qps":
				cts.qps[t] = pt.Value
			case "_success_qps":
				cts.successQ[t] = pt.Value
			case "_failed_qps":
				cts.failedQ[t] = pt.Value
			case "_avg_ms":
				cts.avgMs[t] = pt.Value
			case "_p50_ms":
				cts.p50Ms[t] = pt.Value
			case "_p90_ms":
				cts.p90Ms[t] = pt.Value
			case "_p99_ms":
				cts.p99Ms[t] = pt.Value
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

	for _, caseName := range caseOrder {
		cts := caseMap[caseName]
		sd := chartSeriesData{
			Name:    caseName,
			QPS:     make([]int, n),
			Success: make([]int, n),
			Failed:  make([]int, n),
			AvgMs:   make([]float64, n),
			P50Ms:   make([]int64, n),
			P90Ms:   make([]int64, n),
			P99Ms:   make([]int64, n),
		}
		var totalReqs, totalSuccess, totalFailed int
		var sumAvg float64
		var sumP50, sumP90, sumP99 int64
		var cntP int
		for t, i := range timeIdx {
			q := int(math.Round(cts.qps[t]))
			succ := int(math.Round(cts.successQ[t]))
			fail := int(math.Round(cts.failedQ[t]))
			sd.QPS[i] = q
			sd.Success[i] = succ
			sd.Failed[i] = fail
			sd.AvgMs[i] = math.Round(cts.avgMs[t]*10) / 10
			sd.P50Ms[i] = int64(math.Round(cts.p50Ms[t]))
			sd.P90Ms[i] = int64(math.Round(cts.p90Ms[t]))
			sd.P99Ms[i] = int64(math.Round(cts.p99Ms[t]))
			totalReqs += q
			totalSuccess += succ
			totalFailed += fail
			if q > 0 {
				sumAvg += cts.avgMs[t] * float64(q)
				sumP50 += int64(math.Round(cts.p50Ms[t]))
				sumP90 += int64(math.Round(cts.p90Ms[t]))
				sumP99 += int64(math.Round(cts.p99Ms[t]))
				cntP++
			}
		}
		cd.Series = append(cd.Series, sd)

		// 构建汇总表行（基于每秒指标近似计算）
		cs := caseStat{Name: caseName, Total: totalReqs, Success: totalSuccess, Failed: totalFailed}
		if cntP > 0 {
			cs.AvgMs = math.Round(sumAvg/float64(totalReqs)*10) / 10
			cs.P50Ms = sumP50 / int64(cntP)
			cs.P90Ms = sumP90 / int64(cntP)
			cs.P99Ms = sumP99 / int64(cntP)
		}
		td.CaseStats = append(td.CaseStats, cs)
		td.TotalReqs += totalReqs
		td.SuccessReqs += totalSuccess
		td.FailedReqs += totalFailed
	}

	// 全局延迟近似：取各 case 加权平均
	if td.TotalReqs > 0 {
		var wAvg float64
		var wP50, wP90, wP99 float64
		for _, cs := range td.CaseStats {
			w := float64(cs.Total)
			wAvg += cs.AvgMs * w
			wP50 += float64(cs.P50Ms) * w
			wP90 += float64(cs.P90Ms) * w
			wP99 += float64(cs.P99Ms) * w
		}
		total := float64(td.TotalReqs)
		td.AvgMs = math.Round(wAvg/total*10) / 10
		td.P50Ms = math.Round(wP50/total*10) / 10
		td.P90Ms = math.Round(wP90/total*10) / 10
		td.P99Ms = math.Round(wP99/total*10) / 10
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
		points map[string]float64 // "15:04:05" -> value
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
			t := pt.Timestamp.Format("15:04:05")
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
</style>
</head>
<body>
<div class="hdr">
  <h1>{{.Title}}</h1>
  <div class="meta">Report ID: {{.ReportID}} &nbsp;|&nbsp; {{.StartTime}} — {{.EndTime}} &nbsp;|&nbsp; Generated: {{.CreatedAt}}</div>
</div>
<div class="wrap">
  <div class="cards">
    <div class="cd"><div class="lb">总请求</div><div class="vl inf">{{.TotalReqs}}</div></div>
    <div class="cd"><div class="lb">成功</div><div class="vl ok">{{.SuccessReqs}}</div></div>
    <div class="cd"><div class="lb">失败</div><div class="vl fl">{{.FailedReqs}}</div></div>
    <div class="cd"><div class="lb">Avg</div><div class="vl">{{printf "%.1f" .AvgMs}} ms</div></div>
    <div class="cd"><div class="lb">P50</div><div class="vl">{{printf "%.0f" .P50Ms}} ms</div></div>
    <div class="cd"><div class="lb">P90</div><div class="vl">{{printf "%.0f" .P90Ms}} ms</div></div>
    <div class="cd"><div class="lb">P99</div><div class="vl">{{printf "%.0f" .P99Ms}} ms</div></div>
  </div>

  {{if .CaseStats}}
  <div class="stit">分 Case 统计</div>
  <div class="bx">
    <table class="st">
      <thead><tr><th>Case</th><th>Total</th><th>Success</th><th>Failed</th><th>Avg(ms)</th><th>P50(ms)</th><th>P90(ms)</th><th>P99(ms)</th><th>Min(ms)</th><th>Max(ms)</th></tr></thead>
      <tbody>{{range .CaseStats}}
      <tr><td>{{.Name}}</td><td>{{.Total}}</td><td style="color:#52c41a">{{.Success}}</td><td style="color:#f5222d">{{.Failed}}</td><td>{{printf "%.1f" .AvgMs}}</td><td>{{.P50Ms}}</td><td>{{.P90Ms}}</td><td>{{.P99Ms}}</td><td>{{.MinMs}}</td><td>{{.MaxMs}}</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  {{end}}

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

  <div class="stit">QPS 时间曲线 (点击图例切换显示)</div>
  <div class="bx"><div class="ct" id="c_qps"></div></div>

  <div class="stit">延迟分位数 P50 / P90 / P99 (点击图例切换显示)</div>
  <div class="bx"><div class="ct" id="c_lat"></div></div>

  <div class="stit">成功 / 失败趋势</div>
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
var D={{.ChartDataJSON}};
var EC={{.ErrorCodesJSON}};
var C=['#5470c6','#91cc75','#fac858','#ee6666','#73c0de','#3ba272','#fc8452','#9a60b4','#ea7ccc'];
var charts={};

function mk(id,opt){
  var d=document.getElementById(id);if(!d)return null;
  if(charts[id]){charts[id].dispose();}
  var c=echarts.init(d);c.setOption(opt);
  window.addEventListener('resize',function(){c.resize();});
  charts[id]=c;
  return c;
}

// 初始化 Case 筛选下拉框
var sel=document.getElementById('caseFilter');
if(sel){
  D.series.forEach(function(s){
    var o=document.createElement('option');o.value=s.name;o.textContent=s.name;sel.appendChild(o);
  });
  sel.addEventListener('change',function(){renderCharts(sel.value);});
}

function filterSeries(caseName){
  if(!caseName)return D.series;
  return D.series.filter(function(s){return s.name===caseName;});
}

function renderCharts(caseName){
  var fs=filterSeries(caseName);

  // ---- QPS 曲线 ----
  var qS=[];
  fs.forEach(function(s){
    var i=D.series.indexOf(s);
    qS.push({name:s.name,type:'line',smooth:true,symbol:'circle',symbolSize:3,data:s.qps,
      itemStyle:{color:C[i%C.length]},emphasis:{focus:'series'}});
  });
  mk('c_qps',{
    tooltip:{trigger:'axis'},
    legend:{data:fs.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:40,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:D.timeLabels,axisLabel:{rotate:30,fontSize:11}},
    yAxis:{type:'value',name:'req/s'},
    series:qS
  });

  // ---- 延迟 P50/P90/P99 ----
  var lS=[];
  fs.forEach(function(s){
    var i=D.series.indexOf(s);
    var bc=C[i%C.length];
    lS.push({name:s.name+' P50',type:'line',smooth:true,data:s.p50Ms,lineStyle:{width:1},itemStyle:{color:bc},emphasis:{focus:'series'}});
    lS.push({name:s.name+' P90',type:'line',smooth:true,data:s.p90Ms,lineStyle:{width:2,type:'dashed'},itemStyle:{color:bc},emphasis:{focus:'series'}});
    lS.push({name:s.name+' P99',type:'line',smooth:true,data:s.p99Ms,lineStyle:{width:2,type:'dotted'},itemStyle:{color:bc},emphasis:{focus:'series'}});
  });
  mk('c_lat',{
    tooltip:{trigger:'axis'},
    legend:{data:lS.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:45,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:D.timeLabels,axisLabel:{rotate:30,fontSize:11}},
    yAxis:{type:'value',name:'ms'},
    series:lS
  });

  // ---- 成功 / 失败趋势 ----
  var sfS=[];
  fs.forEach(function(s){
    var i=D.series.indexOf(s);
    sfS.push({name:s.name+' 成功',type:'line',smooth:true,areaStyle:{opacity:.2},data:s.success,
      itemStyle:{color:C[i%C.length]},emphasis:{focus:'series'}});
    sfS.push({name:s.name+' 失败',type:'line',smooth:true,lineStyle:{type:'dashed'},data:s.failed,
      itemStyle:{color:C[(i+3)%C.length]},emphasis:{focus:'series'}});
  });
  mk('c_sf',{
    tooltip:{trigger:'axis'},
    legend:{data:sfS.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:40,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:D.timeLabels,axisLabel:{rotate:30,fontSize:11}},
    yAxis:{type:'value'},
    series:sfS
  });

  // ---- 错误码饼图 ----
  var errData;
  if(!caseName){
    errData=EC;
  }else{
    errData=[];
    fs.forEach(function(s){if(s.errors){errData=errData.concat(s.errors);}});
  }
  mk('c_err',{
    tooltip:{trigger:'item',formatter:'{b}: {c} ({d}%)'},
    series:[{type:'pie',radius:['30%','55%'],data:errData,label:{formatter:'{b}\n{c} ({d}%)'}}]
  });
}

renderCharts('');

// ---- Online Users ----
var OU={{.OnlineUsersJSON}};
if(OU&&OU.series&&OU.series.length>0){
  var ouS=OU.series.map(function(s,i){
    return {name:s.name,type:'line',smooth:true,symbol:'circle',symbolSize:3,data:s.values,
      itemStyle:{color:C[i%C.length]},emphasis:{focus:'series'}};
  });
  mk('c_online_users',{
    tooltip:{trigger:'axis'},
    legend:{data:OU.series.map(function(s){return s.name;}),top:0,type:'scroll',selectedMode:'multiple'},
    grid:{top:40,bottom:35,left:55,right:20},
    xAxis:{type:'category',data:OU.timeLabels,axisLabel:{rotate:30,fontSize:11}},
    yAxis:{type:'value',name:'users'},
    series:ouS
  });
}

// ---- Metrics (dynamic) ----
var M={{.MetricsJSON}};
if(!M)M=[];
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
  renderMetrics();
})();

function renderMetrics(){
  var mc=document.getElementById('mc');if(!mc)return;
  var fn=document.getElementById('metricFilter');fn=fn?fn.value:'';
  var fc=document.getElementById('metricCaseFilter');fc=fc?fc.value:'';
  var fa=document.getElementById('metricAgentFilter');fa=fa?fa.value:'';
  // dispose old
  Object.keys(charts).forEach(function(id){if(id.indexOf('c_mx')===0){charts[id].dispose();delete charts[id];}});
  mc.innerHTML='';
  var fs=M.filter(function(m){
    if(fn&&m.name!==fn)return false;
    if(fc&&m.caseGroup!==fc)return false;
    if(fa&&m.agentGroup!==fa)return false;
    return true;
  });
  if(!fs.length){mc.innerHTML='<div class="empty"><div class="icon">&#x1F4CA;</div><div>No metrics.</div></div>';return;}
  var prevGroup='\x00';
  fs.forEach(function(m,i){
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
      mk(cid,{
        tooltip:{trigger:'axis'},
        grid:{top:20,bottom:35,left:55,right:20},
        xAxis:{type:'category',data:cm.times,axisLabel:{rotate:30,fontSize:11}},
        yAxis:{type:'value'},
        series:[{data:cm.values,type:'line',smooth:true,name:cm.name,areaStyle:{opacity:.12}}]
      });
    })(id,m);
  });
}
})();
</script>
</body>
</html>`
