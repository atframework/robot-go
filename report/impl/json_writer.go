package impl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/atframework/robot-go/report"
)

// JSONFileWriter 将报表数据以 JSON 文件形式写入磁盘
// 目录布局:
//
//	{baseDir}/{reportID}/meta.json
//	{baseDir}/{reportID}/tracings.json
//	{baseDir}/{reportID}/metrics.json
type JSONFileWriter struct {
	mu      sync.Mutex
	baseDir string
}

func NewJSONFileWriter(baseDir string) *JSONFileWriter {
	return &JSONFileWriter{baseDir: baseDir}
}

func (w *JSONFileWriter) WriteTracings(reportID string, records []*report.TracingRecord) error {
	if len(records) == 0 {
		return nil
	}
	return w.appendJSON(reportID, "tracings.json", records)
}

func (w *JSONFileWriter) WriteMetrics(reportID string, series []*report.MetricsSeries) error {
	if len(series) == 0 {
		return nil
	}
	return w.writeJSON(reportID, "metrics.json", series)
}

func (w *JSONFileWriter) WriteMeta(meta *report.ReportMeta) error {
	return w.writeJSON(meta.ReportID, "meta.json", meta)
}

func (w *JSONFileWriter) Close() error {
	return nil
}

// writeJSON 覆盖写入
func (w *JSONFileWriter) writeJSON(reportID, filename string, v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	dir := filepath.Join(w.baseDir, reportID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, filename), data, 0640)
}

// appendJSON 追加写入（读取已有数组 → 合并 → 覆写）
func (w *JSONFileWriter) appendJSON(reportID, filename string, newItems any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	dir := filepath.Join(w.baseDir, reportID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}

	fpath := filepath.Join(dir, filename)

	// 读取现有数据
	var existing []json.RawMessage
	if data, err := os.ReadFile(fpath); err == nil && len(data) > 0 {
		if jsonErr := json.Unmarshal(data, &existing); jsonErr != nil {
			existing = nil
		}
	}

	// 将 newItems（slice）序列化后追加
	newData, err := json.Marshal(newItems)
	if err != nil {
		return fmt.Errorf("marshal new items: %w", err)
	}
	var newRaw []json.RawMessage
	if err := json.Unmarshal(newData, &newRaw); err != nil {
		return fmt.Errorf("unmarshal new items: %w", err)
	}
	existing = append(existing, newRaw...)

	merged, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged: %w", err)
	}
	return os.WriteFile(fpath, merged, 0640)
}

// JSONFileReader 从 JSON 文件读取报表数据
type JSONFileReader struct {
	baseDir string
}

func NewJSONFileReader(baseDir string) *JSONFileReader {
	return &JSONFileReader{baseDir: baseDir}
}

func (r *JSONFileReader) ReadReport(reportID string) (*report.ReportData, error) {
	dir := filepath.Join(r.baseDir, reportID)

	rd := &report.ReportData{}

	// 读 meta
	metaData, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}
	if err := json.Unmarshal(metaData, &rd.Meta); err != nil {
		return nil, fmt.Errorf("unmarshal meta: %w", err)
	}

	// 读 tracings
	if data, err := os.ReadFile(filepath.Join(dir, "tracings.json")); err == nil {
		if err := json.Unmarshal(data, &rd.Tracings); err != nil {
			return nil, fmt.Errorf("unmarshal tracings: %w", err)
		}
	}

	// 读 metrics
	if data, err := os.ReadFile(filepath.Join(dir, "metrics.json")); err == nil {
		if err := json.Unmarshal(data, &rd.Metrics); err != nil {
			return nil, fmt.Errorf("unmarshal metrics: %w", err)
		}
	}

	return rd, nil
}

func (r *JSONFileReader) ListReports() ([]*report.ReportMeta, error) {
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		return nil, fmt.Errorf("list reports dir: %w", err)
	}

	var metas []*report.ReportMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(r.baseDir, entry.Name(), "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta report.ReportMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		metas = append(metas, &meta)
	}
	return metas, nil
}

var _ report.ReportWriter = (*JSONFileWriter)(nil)
var _ report.ReportReader = (*JSONFileReader)(nil)
