// Package mqclient 是 Redis Streams 的薄封装（scanmq → Redis 迁移版）。
// 接口与旧版保持一致：Add / Read / Ack，业务代码零改动。
//
// 语义映射（scanmq → Redis Streams）：
//
//	MQADD          → XADD <stream> * p <payload>
//	MQREAD BLOCK   → XREADGROUP GROUP g c COUNT n BLOCK ms STREAMS s >
//	                 空时补 XAUTOCLAIM（接管超时未确认消息，语义同 PEL 重投）
//	MQACK          → XACK
//	重投计数/DLQ    → XPENDING 的 DeliveryCount，由 Janitor 处理
package mqclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Message struct {
	ID      string
	Payload string
}

type Client struct {
	rdb *redis.Client
	// ReclaimIdle 非零时：读不到新消息就用 XAUTOCLAIM 接管闲置超期的消息
	ReclaimIdle time.Duration
}

func Dial(addr, password string) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb, ReclaimIdle: 60 * time.Second}, nil
}

func (c *Client) Close() error { return c.rdb.Close() }

func (c *Client) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.rdb.Ping(ctx).Err()
}

// Add 入队，返回消息 ID（Redis 的 ms-seq 格式）。
func (c *Client) Add(stream, payload string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"p": payload},
	}).Result()
}

// Read 消费组读取：先发新消息；没有则按 ReclaimIdle 接管超时消息。
// blockMs=0 表示不阻塞。
func (c *Client) Read(stream, group, consumer string, count, blockMs int) ([]Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(blockMs)*time.Millisecond+10*time.Second)
	defer cancel()
	if err := c.ensureGroup(ctx, stream, group); err != nil {
		return nil, err
	}
	streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    int64(count),
		Block:    time.Duration(blockMs) * time.Millisecond,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if len(streams) > 0 && len(streams[0].Messages) > 0 {
		return toMessages(streams[0].Messages), nil
	}
	// 无新消息：接管闲置超期的 PEL 消息（delivery count 由 Redis 自增）
	if c.ReclaimIdle > 0 {
		msgs, _, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   stream,
			Group:    group,
			Consumer: consumer,
			MinIdle:  c.ReclaimIdle,
			Start:    "0-0",
			Count:    int64(count),
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, err
		}
		return toMessages(msgs), nil
	}
	return nil, nil
}

// Ack 确认消息。
func (c *Client) Ack(stream, group, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.rdb.XAck(ctx, stream, group, id).Err()
}

// ListStreams 按 glob 模式列出流名（SCAN MATCH）。
func (c *Client) ListStreams(pattern string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out []string
	iter := c.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		out = append(out, iter.Val())
	}
	return out, iter.Err()
}

// ListGroups 列出一条流上的所有消费组。
func (c *Client) ListGroups(stream string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	groups, err := c.rdb.XInfoGroups(ctx, stream).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Name)
	}
	return out, nil
}

// SweepDLQ 把投递次数达上限的消息搬进 <stream>.dlq，返回搬运数。
// 由后台 janitor 周期调用（替代 scanmq 内建 sweeper）。
func (c *Client) SweepDLQ(stream, group string, maxDeliveries int64) (int, error) {
	pending, err := c.Pending(stream, group, 1000)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, p := range pending {
		if p.Deliveries < maxDeliveries {
			continue
		}
		payload, err := c.ReadByID(stream, p.ID)
		if err != nil {
			c.Ack(stream, group, p.ID) // 原消息已丢，PEL 不留垃圾
			continue
		}
		dlqPayload := fmt.Sprintf(`{"_dlq_reason":"max_deliveries(%d) from %s:%s","payload":%s}`,
			p.Deliveries, stream, p.ID, payload)
		if _, err := c.Add(stream+".dlq", dlqPayload); err != nil {
			return moved, err
		}
		if err := c.Ack(stream, group, p.ID); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// PendingInfo 列出组内待确认消息（含投递次数），供 Janitor 判 DLQ。
type PendingInfo struct {
	ID         string
	Consumer   string
	Idle       time.Duration
	Deliveries int64
}

func (c *Client) Pending(stream, group string, count int64) ([]PendingInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ext, err := c.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stream, Group: group, Start: "-", End: "+", Count: count,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]PendingInfo, 0, len(ext))
	for _, e := range ext {
		out = append(out, PendingInfo{
			ID: e.ID, Consumer: e.Consumer, Idle: e.Idle, Deliveries: e.RetryCount,
		})
	}
	return out, nil
}

// ReadByID 按 ID 取消息（DLQ 搬运用）。
func (c *Client) ReadByID(stream, id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgs, err := c.rdb.XRangeN(ctx, stream, id, id, 1).Result()
	if err != nil || len(msgs) == 0 {
		return "", fmt.Errorf("message %s not found", id)
	}
	payload, _ := msgs[0].Values["p"].(string)
	return payload, nil
}

// ensureGroup 惰性创建消费组（BUSYGROUP 忽略）。
func (c *Client) ensureGroup(ctx context.Context, stream, group string) error {
	err := c.rdb.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !errors.Is(err, redis.Nil) &&
		!strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func toMessages(xs []redis.XMessage) []Message {
	out := make([]Message, 0, len(xs))
	for _, m := range xs {
		payload, _ := m.Values["p"].(string)
		out = append(out, Message{ID: m.ID, Payload: payload})
	}
	return out
}
