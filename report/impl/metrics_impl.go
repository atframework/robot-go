package impl

import (
	"sync"
	"time"

	"github.com/atframework/robot-go/report"
)

// metricsRegistration 记录一个已注册的指标采集项
type metricsRegistration struct {
	name   string
	labels map[string]string
	fn     report.MetricsValueFunc
}

// registrationKey 根据 name+labels 生成唯一键
func registrationKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	// 简单拼接，保证同 name 不同 labels 能区分
	key := name
	for k, v := range labels {
		key += "\x00" + k + "=" + v
	}
	return key
}

// MemoryMetricsCollector 是 MetricsCollector 接口的内存实现
type MemoryMetricsCollector struct {
	mu            sync.Mutex
	registrations map[string]*metricsRegistration  // key → registration
	series        map[string]*report.MetricsSeries // key → 时间序列
	stopCh        chan struct{}
	running       bool
}

func NewMemoryMetricsCollector() *MemoryMetricsCollector {
	return &MemoryMetricsCollector{
		registrations: make(map[string]*metricsRegistration),
		series:        make(map[string]*report.MetricsSeries),
	}
}

func (c *MemoryMetricsCollector) Register(name string, fn report.MetricsValueFunc) {
	c.RegisterWithLabels(name, nil, fn)
}

func (c *MemoryMetricsCollector) RegisterWithLabels(name string, labels map[string]string, fn report.MetricsValueFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := registrationKey(name, labels)
	c.registrations[key] = &metricsRegistration{name: name, labels: labels, fn: fn}
	if _, exists := c.series[key]; !exists {
		c.series[key] = &report.MetricsSeries{Name: name, Labels: labels}
	}
}

func (c *MemoryMetricsCollector) Unregister(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := registrationKey(name, nil)
	delete(c.registrations, key)
}

func (c *MemoryMetricsCollector) Collect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 截断到秒：同一秒内的多次采集只保留最后一个
	now := time.Now().Truncate(time.Second)
	for key, reg := range c.registrations {
		value := reg.fn()
		s := c.series[key]
		if s == nil {
			s = &report.MetricsSeries{Name: reg.name, Labels: reg.labels}
			c.series[key] = s
		}
		if n := len(s.Points); n > 0 && s.Points[n-1].Timestamp.Equal(now) {
			// 同一秒内的再次采集覆盖旧值
			s.Points[n-1].Value = value
		} else {
			s.Points = append(s.Points, report.MetricsPoint{Timestamp: now, Value: value})
		}
	}
}

func (c *MemoryMetricsCollector) StartAutoCollect(interval time.Duration) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.stopCh = make(chan struct{})
	c.running = true
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.Collect()
			case <-c.stopCh:
				return
			}
		}
	}()
}

func (c *MemoryMetricsCollector) StopAutoCollect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		close(c.stopCh)
		c.running = false
	}
}

func (c *MemoryMetricsCollector) Snapshot() []*report.MetricsSeries {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]*report.MetricsSeries, 0, len(c.series))
	for _, s := range c.series {
		cp := &report.MetricsSeries{
			Name:   s.Name,
			Labels: s.Labels,
			Points: make([]report.MetricsPoint, len(s.Points)),
		}
		copy(cp.Points, s.Points)
		result = append(result, cp)
	}
	return result
}

func (c *MemoryMetricsCollector) Flush() []*report.MetricsSeries {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]*report.MetricsSeries, 0, len(c.series))
	for key, s := range c.series {
		result = append(result, s)
		c.series[key] = &report.MetricsSeries{Name: s.Name, Labels: s.Labels}
	}
	return result
}

func (c *MemoryMetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.series = make(map[string]*report.MetricsSeries)
}

var _ report.MetricsCollector = (*MemoryMetricsCollector)(nil)
