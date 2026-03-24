package report

import (
	"fmt"
	"path/filepath"
	"time"
)

// ReportManager 持有报表系统的全部组件，在整个 Robot 生命周期内使用
type ReportManager struct {
	Tracer    Tracer
	Metrics   MetricsCollector
	Pressure  PressureController
	Writer    ReportWriter
	Generator HTMLGenerator
	ReportDir string
	ReportID  string
	StartTime time.Time
}

var globalManager *ReportManager

// SetGlobalManager 设置全局报表管理器（由 robot.go 在启动时调用）
func SetGlobalManager(m *ReportManager) { globalManager = m }

// GetGlobalManager 获取全局报表管理器（case 执行时自动读取）
func GetGlobalManager() *ReportManager { return globalManager }

func FinalizeReport() {
	mgr := GetGlobalManager()
	if mgr == nil {
		return
	}

	// 最后强制采集一次，确保短时 case 也能留下数据点
	mgr.Metrics.Collect()
	// 停止自动采集
	mgr.Metrics.StopAutoCollect()

	now := time.Now()
	meta := &ReportMeta{
		ReportID:  mgr.ReportID,
		Title:     "Robot Stress Test Report",
		StartTime: mgr.StartTime,
		EndTime:   now,
		CreatedAt: now,
	}

	tracings := mgr.Tracer.Flush()
	metricsData := mgr.Metrics.Flush()

	// 打点清洗为每秒指标
	cleanedMetrics := CleanTracingsToMetrics(tracings)
	metricsData = append(metricsData, cleanedMetrics...)

	// 追加 pressure 快照为 metrics 序列
	if mgr.Pressure != nil {
		snapshots := mgr.Pressure.Snapshots()
		if len(snapshots) > 0 {
			var pressurePts, throttlePts, actualQPSPts []MetricsPoint
			for _, s := range snapshots {
				pressurePts = append(pressurePts, MetricsPoint{Timestamp: s.Timestamp, Value: float64(s.Level)})
				throttlePts = append(throttlePts, MetricsPoint{Timestamp: s.Timestamp, Value: s.ThrottleRatio})
				actualQPSPts = append(actualQPSPts, MetricsPoint{Timestamp: s.Timestamp, Value: s.ActualQPS})
			}
			metricsData = append(metricsData,
				&MetricsSeries{Name: "pressure_level", Points: pressurePts},
				&MetricsSeries{Name: "throttle_ratio", Points: throttlePts},
				&MetricsSeries{Name: "actual_qps", Points: actualQPSPts},
			)
		}
	}

	// 写入 JSON（仅备份元数据和清洗后的指标，不写入原始打点数据以减小磁盘占用）
	if err := mgr.Writer.WriteMeta(meta); err != nil {
		fmt.Println("Write report meta error:", err)
	}
	if err := mgr.Writer.WriteMetrics(mgr.ReportID, metricsData); err != nil {
		fmt.Println("Write metrics error:", err)
	}
	mgr.Writer.Close()

	// 生成 HTML
	data := &ReportData{Meta: *meta, Tracings: tracings, Metrics: metricsData}
	htmlPath := filepath.Join(mgr.ReportDir, mgr.ReportID, "report.html")
	if err := mgr.Generator.GenerateToFile(data, htmlPath); err != nil {
		fmt.Println("Generate HTML report error:", err)
	} else {
		fmt.Println("Report generated:", htmlPath)
	}
}
