// essaybatch.go 作文批次聚合器：等一个学生的作文全部到齐后再进队列。
//
// 数据结构（Redis）：
//
//	essay_pending:<uuid>   BITMAP  页到位图：bit N = 物理页 N+1 已分类完成
//	essay_expected:<uuid>  SET     应到作文物理页集合（来自试卷模板）
//	essay_pages:<uuid>     HASH    物理页码 -> RustFS key（页数据）
//
// 判齐不依赖客户端字段：expected 由试卷模板的作文大题页给出（每次 AddPage
// 幂等登记），bitmap 到位数达到集合大小即发出；闲置超时由 Sweep 残缺兜底。
package classify

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// EssayUnit 是一篇组装完成的作文（1~N 页，按扫描序）。
type EssayUnit struct {
	UUID         string   `json:"uuid"`
	Keys         []string `json:"keys"`          // RustFS keys，按扫描序
	PagesMissing bool     `json:"pages_missing"` // 超时残缺发出
}

// EssayBatcher 聚合作文页。所有 key 带 TTL 防泄漏。
type EssayBatcher struct {
	rdb         *redis.Client
	idleTimeout time.Duration
	ttl         time.Duration
}

func NewEssayBatcher(rdb *redis.Client, idleTimeout time.Duration) *EssayBatcher {
	return &EssayBatcher{rdb: rdb, idleTimeout: idleTimeout, ttl: 10 * time.Minute}
}

func (b *EssayBatcher) pendingKey(uuid string) string  { return "essay_pending:" + uuid }
func (b *EssayBatcher) expectedKey(uuid string) string { return "essay_expected:" + uuid }
func (b *EssayBatcher) pagesKey(uuid string) string    { return "essay_pages:" + uuid }
func (b *EssayBatcher) activityKey(uuid string) string { return "essay_activity:" + uuid }

// AddPage 登记一页（分类完成的作文页）。expected 为试卷模板给出的作文物理页
// 集合（每次调用幂等重登，模板晚可用时后到的页会补上）。齐了返回组装完成的
// unit，未齐返回 nil。
func (b *EssayBatcher) AddPage(ctx context.Context, uuid string, page int, expected []int, key string) (*EssayUnit, error) {
	pipe := b.rdb.TxPipeline()
	pipe.HSet(ctx, b.pagesKey(uuid), page, key)
	pipe.SetBit(ctx, b.pendingKey(uuid), int64(page-1), 1)
	if len(expected) > 0 {
		members := make([]any, 0, len(expected))
		for _, p := range expected {
			members = append(members, p)
		}
		pipe.SAdd(ctx, b.expectedKey(uuid), members...)
	}
	pipe.Set(ctx, b.activityKey(uuid), time.Now().Unix(), b.ttl)
	pipe.Expire(ctx, b.pendingKey(uuid), b.ttl)
	pipe.Expire(ctx, b.pagesKey(uuid), b.ttl)
	pipe.Expire(ctx, b.expectedKey(uuid), b.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return b.tryFlush(ctx, uuid)
}

// tryFlush 检查是否齐：expected 已登记且 bitmap 到位数达到集合大小 → 发出。
func (b *EssayBatcher) tryFlush(ctx context.Context, uuid string) (*EssayUnit, error) {
	expected, err := b.expected(ctx, uuid)
	if err != nil || expected <= 0 {
		return nil, err
	}
	arrived, err := b.arrivedCount(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if arrived < expected {
		return nil, nil
	}
	return b.flush(ctx, uuid, false)
}

// arrivedCount 数 bitmap 里有几个 1。位按物理页码设置且作文页必属于
// expected 集合，因此计数不超过集合大小，相等即齐（异常多出计数时宽容判齐）。
func (b *EssayBatcher) arrivedCount(ctx context.Context, uuid string) (int, error) {
	count, err := b.rdb.BitCount(ctx, b.pendingKey(uuid), nil).Result()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// expected 应到作文页数（模板给出的物理页集合大小；0 = 模板未就绪，不判齐）。
func (b *EssayBatcher) expected(ctx context.Context, uuid string) (int, error) {
	n, err := b.rdb.SCard(ctx, b.expectedKey(uuid)).Result()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// flush 发出 unit 并清理状态。missing=true 表示超时残缺发出。
func (b *EssayBatcher) flush(ctx context.Context, uuid string, missing bool) (*EssayUnit, error) {
	pages, err := b.rdb.HGetAll(ctx, b.pagesKey(uuid)).Result()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, nil
	}
	orders := make([]int, 0, len(pages))
	for k := range pages {
		n, _ := strconv.Atoi(k)
		orders = append(orders, n)
	}
	sort.Ints(orders)
	unit := &EssayUnit{UUID: uuid, PagesMissing: missing}
	for _, n := range orders {
		unit.Keys = append(unit.Keys, pages[strconv.Itoa(n)])
	}
	pipe := b.rdb.TxPipeline()
	pipe.Del(ctx, b.pendingKey(uuid), b.expectedKey(uuid), b.pagesKey(uuid), b.activityKey(uuid))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return unit, nil
}

// Sweep 兜底清扫：闲置超时的 uuid 残缺发出。由 janitor 周期调用。
func (b *EssayBatcher) Sweep(ctx context.Context) ([]*EssayUnit, error) {
	var flushed []*EssayUnit
	var cursor uint64
	for {
		keys, next, err := b.rdb.Scan(ctx, cursor, "essay_activity:*", 100).Result()
		if err != nil {
			return flushed, err
		}
		for _, key := range keys {
			uuid := key[len("essay_activity:"):]
			ts, err := b.rdb.Get(ctx, key).Int64()
			if err != nil {
				continue
			}
			if time.Since(time.Unix(ts, 0)) < b.idleTimeout {
				continue
			}
			unit, err := b.flush(ctx, uuid, true)
			if err == nil && unit != nil {
				flushed = append(flushed, unit)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return flushed, nil
}

// Status 调试用：某 uuid 的聚合状态。
func (b *EssayBatcher) Status(ctx context.Context, uuid string) (string, error) {
	expected, _ := b.expected(ctx, uuid)
	bits, _ := b.rdb.BitCount(ctx, b.pendingKey(uuid), nil).Result()
	pages, _ := b.rdb.HLen(ctx, b.pagesKey(uuid)).Result()
	return fmt.Sprintf("expected=%d arrived=%d pages=%d", expected, bits, pages), nil
}
