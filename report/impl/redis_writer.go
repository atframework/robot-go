package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/atframework/robot-go/report"
)

// RedisReportWriter 通过 Redis 写入报表数据（Agent 端使用）。
// Key 规范:
//
//	report:meta:{reportID}              → String (JSON)
//	report:tracing:{reportID}:{agentID} → List   (JSON 分块)
//	report:metrics:{reportID}:{agentID} → List   (JSON 分块)
//	report:index                        → SortedSet (member=reportID, score=unix)
type RedisReportWriter struct {
	client  *redis.Client
	agentID string
}

// NewRedisReportWriter 创建基于 Redis 的 ReportWriter。
func NewRedisReportWriter(client *redis.Client, agentID string) *RedisReportWriter {
	return &RedisReportWriter{client: client, agentID: agentID}
}

func (w *RedisReportWriter) WriteTracings(reportID string, records []*report.TracingRecord) error {
	if len(records) == 0 {
		return nil
	}
	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("marshal tracings: %w", err)
	}
	key := fmt.Sprintf("report:tracing:%s:%s", reportID, w.agentID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return w.client.RPush(ctx, key, string(data)).Err()
}

func (w *RedisReportWriter) WriteMetrics(reportID string, series []*report.MetricsSeries) error {
	if len(series) == 0 {
		return nil
	}
	data, err := json.Marshal(series)
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	key := fmt.Sprintf("report:metrics:%s:%s", reportID, w.agentID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return w.client.RPush(ctx, key, string(data)).Err()
}

func (w *RedisReportWriter) WriteMeta(meta *report.ReportMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("report:meta:%s", meta.ReportID)
	if err := w.client.Set(ctx, key, string(data), 0).Err(); err != nil {
		return err
	}
	return w.client.ZAdd(ctx, "report:index", redis.Z{
		Score:  float64(meta.CreatedAt.Unix()),
		Member: meta.ReportID,
	}).Err()
}

func (w *RedisReportWriter) Close() error {
	return nil // client 由外部管理
}

// BarrierACK 当前 agent 向 barrier 集合写入 ACK，表示该 case 步骤执行完成。
func (w *RedisReportWriter) BarrierACK(reportID string, caseIndex int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := fmt.Sprintf("task:barrier:%s:%d", reportID, caseIndex)
	return w.client.SAdd(ctx, key, w.agentID).Err()
}

var _ report.ReportWriter = (*RedisReportWriter)(nil)

// GenerateUniqueReportID 使用 Redis INCR 生成全局唯一的 ReportID。
// 格式：{timestamp}-{seq}，其中 seq 由 Redis key "report:id:seq" 自增获得。
func GenerateUniqueReportID(client *redis.Client) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	seq, err := client.Incr(ctx, "report:id:seq").Result()
	if err != nil {
		return "", fmt.Errorf("redis INCR report:id:seq: %w", err)
	}
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), seq), nil
}
