package dbtool

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	pu "github.com/atframework/atframe-utils-go/proto_utility"
	redis_interface "github.com/atframework/robot-go/redis"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Querier 负责从 Redis 读取数据库表数据并格式化输出
type Querier struct {
	client       redis_interface.RedisClient
	registry     *Registry
	recordPrefix string
}

// NewQuerier 创建查询器
func NewQuerier(client redis_interface.RedisClient, registry *Registry, recordPrefix string) *Querier {
	return &Querier{
		client:       client,
		registry:     registry,
		recordPrefix: recordPrefix,
	}
}

// BuildRedisKey 根据 table index 和 key values 构建 Redis key
func (q *Querier) BuildRedisKey(index *TableIndex, keyValues []string) string {
	parts := make([]string, 0, len(keyValues))
	parts = append(parts, fmt.Sprintf("%s-%s", q.recordPrefix, index.Name))
	for _, v := range keyValues {
		parts[0] = parts[0] + "." + v
	}
	return parts[0]
}

// QueryKV 查询 KV 类型表
func (q *Querier) QueryKV(info *TableInfo, index *TableIndex, keyValues []string) (string, error) {
	redisKey := q.BuildRedisKey(index, keyValues)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data, err := q.client.HGetAll(ctx, redisKey).Result()
	if err != nil {
		return "", fmt.Errorf("HGETALL %s: %w", redisKey, err)
	}
	if len(data) == 0 {
		return fmt.Sprintf("(empty) key: %s", redisKey), nil
	}

	msg := dynamicpb.NewMessage(info.MessageDesc)
	casVersion, err := pu.RedisKVMapToPB(data, msg)
	if err != nil {
		return "", fmt.Errorf("parse proto from redis: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== %s [KV] key: %s ===\n", index.Name, redisKey))
	if index.EnableCAS {
		sb.WriteString(fmt.Sprintf("CAS Version: %d\n", casVersion))
	}
	sb.WriteString(pu.MessageReadableText(msg))
	return sb.String(), nil
}

// QueryKL 查询 KL (Key-List) 类型表
func (q *Querier) QueryKL(info *TableInfo, index *TableIndex, keyValues []string, listIndex int64) (string, error) {
	redisKey := q.BuildRedisKey(index, keyValues)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if listIndex >= 0 {
		// 查询单条
		return q.queryKLSingle(ctx, info, index, redisKey, uint64(listIndex))
	}

	// 查询全部
	data, err := q.client.HGetAll(ctx, redisKey).Result()
	if err != nil {
		return "", fmt.Errorf("HGETALL %s: %w", redisKey, err)
	}
	if len(data) == 0 {
		return fmt.Sprintf("(empty) key: %s", redisKey), nil
	}

	results, err := pu.RedisKLMapToPB(data, func() proto.Message {
		return dynamicpb.NewMessage(info.MessageDesc)
	})
	if err != nil {
		return "", fmt.Errorf("parse KL from redis: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== %s [KL] key: %s, total: %d ===\n", index.Name, redisKey, len(results)))
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("--- index: %d ---\n", r.ListIndex))
		if r.Table != nil {
			sb.WriteString(pu.MessageReadableText(r.Table))
		} else {
			sb.WriteString("(nil)")
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// queryKLSingle 查询 KL 单条
func (q *Querier) queryKLSingle(ctx context.Context, info *TableInfo, index *TableIndex, redisKey string, listIdx uint64) (string, error) {
	field := strconv.FormatUint(listIdx, 10)
	val, err := q.client.HGet(ctx, redisKey, field).Result()
	if err == redis.Nil {
		return fmt.Sprintf("(not found) key: %s index: %d", redisKey, listIdx), nil
	}
	if err != nil {
		return "", fmt.Errorf("HGET %s %s: %w", redisKey, field, err)
	}

	if len(val) <= 1 || val[0] != '&' {
		return fmt.Sprintf("(invalid data) key: %s index: %d", redisKey, listIdx), nil
	}

	msg := dynamicpb.NewMessage(info.MessageDesc)
	if err := (proto.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(val[1:]), msg); err != nil {
		return "", fmt.Errorf("unmarshal KL entry: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== %s [KL] key: %s index: %d ===\n", index.Name, redisKey, listIdx))
	sb.WriteString(pu.MessageReadableText(msg))
	return sb.String(), nil
}

// QuerySortedSetCount 查询 SORTED_SET 数据量
func (q *Querier) QuerySortedSetCount(index *TableIndex, keyValues []string) (string, error) {
	redisKey := q.BuildRedisKey(index, keyValues)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := q.client.ZCard(ctx, redisKey).Result()
	if err != nil {
		return "", fmt.Errorf("ZCARD %s: %w", redisKey, err)
	}

	return fmt.Sprintf("=== %s [SORTED_SET] key: %s count: %d ===", index.Name, redisKey, count), nil
}

// QuerySortedSetByRank 按排名范围查询 SORTED_SET
func (q *Querier) QuerySortedSetByRank(index *TableIndex, keyValues []string, start, stop int64, reverse bool) (string, error) {
	redisKey := q.BuildRedisKey(index, keyValues)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var members []redis.Z
	var err error
	if reverse {
		members, err = q.client.ZRevRangeWithScores(ctx, redisKey, start, stop).Result()
	} else {
		members, err = q.client.ZRangeWithScores(ctx, redisKey, start, stop).Result()
	}
	if err != nil {
		return "", fmt.Errorf("ZRANGE %s: %w", redisKey, err)
	}

	var sb strings.Builder
	order := "ASC"
	if reverse {
		order = "DESC"
	}
	sb.WriteString(fmt.Sprintf("=== %s [SORTED_SET] key: %s rank[%d,%d] %s, count: %d ===\n",
		index.Name, redisKey, start, stop, order, len(members)))
	for i, m := range members {
		sb.WriteString(fmt.Sprintf("  #%d  member: %v  score: %g\n", start+int64(i), m.Member, m.Score))
	}
	return sb.String(), nil
}

// QuerySortedSetByScore 按分数范围查询 SORTED_SET
func (q *Querier) QuerySortedSetByScore(index *TableIndex, keyValues []string, min, max string, offset, count int64, reverse bool) (string, error) {
	redisKey := q.BuildRedisKey(index, keyValues)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var members []redis.Z
	var err error
	if reverse {
		members, err = q.client.ZRevRangeByScoreWithScores(ctx, redisKey, &redis.ZRangeBy{
			Min:    min,
			Max:    max,
			Offset: offset,
			Count:  count,
		}).Result()
	} else {
		members, err = q.client.ZRangeByScoreWithScores(ctx, redisKey, &redis.ZRangeBy{
			Min:    min,
			Max:    max,
			Offset: offset,
			Count:  count,
		}).Result()
	}
	if err != nil {
		return "", fmt.Errorf("ZRANGEBYSCORE %s: %w", redisKey, err)
	}

	var sb strings.Builder
	order := "ASC"
	if reverse {
		order = "DESC"
	}
	sb.WriteString(fmt.Sprintf("=== %s [SORTED_SET] key: %s score[%s,%s] %s offset=%d count=%d, results: %d ===\n",
		index.Name, redisKey, min, max, order, offset, count, len(members)))
	for i, m := range members {
		sb.WriteString(fmt.Sprintf("  #%d  member: %v  score: %g\n", i, m.Member, m.Score))
	}
	return sb.String(), nil
}
