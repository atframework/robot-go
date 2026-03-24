package report

import (
	"math"
	"sort"
	"time"
)

// CleanTracingsToMetrics 将原始打点记录按 case 名称分组，逐秒聚合为标准 MetricsSeries。
// 对每个 caseName 生成:
//
//	{name}_qps, {name}_success_qps, {name}_failed_qps,
//	{name}_avg_ms, {name}_p50_ms, {name}_p90_ms, {name}_p99_ms, {name}_success_rate
func CleanTracingsToMetrics(records []*TracingRecord) []*MetricsSeries {
	if len(records) == 0 {
		return nil
	}

	nameOrder := make([]string, 0)
	grouped := make(map[string][]*TracingRecord)
	for _, r := range records {
		if _, exists := grouped[r.Name]; !exists {
			nameOrder = append(nameOrder, r.Name)
		}
		grouped[r.Name] = append(grouped[r.Name], r)
	}

	var result []*MetricsSeries

	for _, name := range nameOrder {
		recs := grouped[name]

		type bucket struct {
			success int
			failed  int
			totalMs int64
			durs    []int64
		}
		buckets := make(map[int64]*bucket)
		for _, r := range recs {
			sec := r.EndTime.Unix()
			b := buckets[sec]
			if b == nil {
				b = &bucket{}
				buckets[sec] = b
			}
			b.totalMs += r.DurationMs
			b.durs = append(b.durs, r.DurationMs)
			if r.Code == TracingSuccess {
				b.success++
			} else {
				b.failed++
			}
		}

		secs := make([]int64, 0, len(buckets))
		for s := range buckets {
			secs = append(secs, s)
		}
		sort.Slice(secs, func(i, j int) bool { return secs[i] < secs[j] })

		labels := map[string]string{"case": name}
		qpsSeries := &MetricsSeries{Name: name + "_qps", Labels: labels}
		successQPSSeries := &MetricsSeries{Name: name + "_success_qps", Labels: labels}
		failedQPSSeries := &MetricsSeries{Name: name + "_failed_qps", Labels: labels}
		avgSeries := &MetricsSeries{Name: name + "_avg_ms", Labels: labels}
		p50Series := &MetricsSeries{Name: name + "_p50_ms", Labels: labels}
		p90Series := &MetricsSeries{Name: name + "_p90_ms", Labels: labels}
		p99Series := &MetricsSeries{Name: name + "_p99_ms", Labels: labels}
		rateSeries := &MetricsSeries{Name: name + "_success_rate", Labels: labels}

		for _, sec := range secs {
			b := buckets[sec]
			ts := time.Unix(sec, 0)
			total := b.success + b.failed

			sort.Slice(b.durs, func(i, j int) bool { return b.durs[i] < b.durs[j] })
			avg := float64(b.totalMs) / float64(total)

			qpsSeries.Points = append(qpsSeries.Points, MetricsPoint{Timestamp: ts, Value: float64(total)})
			successQPSSeries.Points = append(successQPSSeries.Points, MetricsPoint{Timestamp: ts, Value: float64(b.success)})
			failedQPSSeries.Points = append(failedQPSSeries.Points, MetricsPoint{Timestamp: ts, Value: float64(b.failed)})
			avgSeries.Points = append(avgSeries.Points, MetricsPoint{Timestamp: ts, Value: math.Round(avg*10) / 10})
			p50Series.Points = append(p50Series.Points, MetricsPoint{Timestamp: ts, Value: float64(pct(b.durs, 50))})
			p90Series.Points = append(p90Series.Points, MetricsPoint{Timestamp: ts, Value: float64(pct(b.durs, 90))})
			p99Series.Points = append(p99Series.Points, MetricsPoint{Timestamp: ts, Value: float64(pct(b.durs, 99))})

			rate := 0.0
			if total > 0 {
				rate = math.Round(float64(b.success)/float64(total)*10000) / 100
			}
			rateSeries.Points = append(rateSeries.Points, MetricsPoint{Timestamp: ts, Value: rate})
		}

		result = append(result, qpsSeries, successQPSSeries, failedQPSSeries, avgSeries, p50Series, p90Series, p99Series, rateSeries)
	}

	return result
}

func pct(sorted []int64, p int) int64 {
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
