package report

import (
	"encoding/json"
	"time"
)

// metricsSeriesWire 是 MetricsSeries 的 JSON 序列化中间格式。
// 等间隔序列使用紧凑格式（start + step_s + values），
// 不规则序列回退到原始 points 格式（向后兼容）。
type metricsSeriesWire struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	// 紧凑格式字段（等间隔序列）
	Start  *time.Time `json:"start,omitempty"`
	StepS  *int64     `json:"step_s,omitempty"` // 整数秒，当前格式
	Values []float64  `json:"values,omitempty"`
	// 稀疏格式字段（不规则序列，向后兼容）
	Points []MetricsPoint `json:"points,omitempty"`
	// 兼容旧格式，仅反序列化时读取
	StepMs *int64 `json:"step_ms,omitempty"`
}

// MarshalJSON 序列化：>=2 个点且间隔规律时使用紧凑格式，否则使用 points 格式。
func (s *MetricsSeries) MarshalJSON() ([]byte, error) {
	w := metricsSeriesWire{Name: s.Name, Labels: s.Labels}
	if stepS, ok := detectRegularInterval(s.Points); ok {
		t := s.Points[0].Timestamp.Truncate(time.Second)
		w.Start = &t
		w.StepS = &stepS
		w.Values = make([]float64, len(s.Points))
		for i, p := range s.Points {
			w.Values[i] = p.Value
		}
	} else {
		pts := make([]MetricsPoint, len(s.Points))
		for i, p := range s.Points {
			pts[i] = MetricsPoint{Timestamp: p.Timestamp.Truncate(time.Second), Value: p.Value}
		}
		w.Points = pts
	}
	return json.Marshal(w)
}

// UnmarshalJSON 反序列化：同时支持紧凑格式（step_s / step_ms）和原始 points 格式。
func (s *MetricsSeries) UnmarshalJSON(data []byte) error {
	var w metricsSeriesWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	s.Name = w.Name
	s.Labels = w.Labels
	if w.Start != nil && len(w.Values) > 0 {
		var step time.Duration
		switch {
		case w.StepS != nil:
			step = time.Duration(*w.StepS) * time.Second
		case w.StepMs != nil:
			step = time.Duration(*w.StepMs) * time.Millisecond // 兼容旧数据
		default:
			step = time.Second
		}
		s.Points = make([]MetricsPoint, len(w.Values))
		for i, v := range w.Values {
			s.Points[i] = MetricsPoint{
				Timestamp: w.Start.Add(time.Duration(i) * step),
				Value:     v,
			}
		}
	} else {
		s.Points = w.Points
	}
	return nil
}

// detectRegularInterval 检测是否为等间隔序列（秒精度）。
// 返回 (stepS, true) 表示等间隔，序列长度 < 2 或间隔不一致时返回 (0, false)。
func detectRegularInterval(points []MetricsPoint) (stepS int64, ok bool) {
	if len(points) < 2 {
		return 0, false
	}
	base := points[1].Timestamp.Truncate(time.Second).Unix() - points[0].Timestamp.Truncate(time.Second).Unix()
	if base <= 0 {
		return 0, false
	}
	for i := 2; i < len(points); i++ {
		diff := points[i].Timestamp.Truncate(time.Second).Unix() - points[i-1].Timestamp.Truncate(time.Second).Unix()
		if diff != base {
			return 0, false
		}
	}
	return base, true
}
