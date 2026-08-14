package tests

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"scanpipe/internal/classify"
)

func testRedis(t *testing.T) *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	return rdb
}

func cleanupKeys(t *testing.T, rdb *redis.Client, uuid string) {
	t.Cleanup(func() {
		rdb.Del(context.Background(),
			"essay_pending:"+uuid, "essay_expected:"+uuid,
			"essay_pages:"+uuid, "essay_activity:"+uuid)
	})
}

func TestEssayBatchThreePages(t *testing.T) {
	rdb := testRedis(t)
	b := classify.NewEssayBatcher(rdb, time.Minute)
	ctx := context.Background()
	uuid := "test-uuid-3p"
	cleanupKeys(t, rdb, uuid)
	exp := []int{1, 2, 3} // 模板给出的作文物理页集合

	u, _ := b.AddPage(ctx, uuid, 1, exp, "essay/u/page_1.jpg")
	if u != nil {
		t.Fatal("1页就发了？")
	}
	u, _ = b.AddPage(ctx, uuid, 2, exp, "essay/u/page_2.jpg")
	if u != nil {
		t.Fatal("2/3 就发了？")
	}
	u, err := b.AddPage(ctx, uuid, 3, exp, "essay/u/page_3.jpg")
	if err != nil || u == nil {
		t.Fatalf("3/3 应该发出, err=%v unit=%v", err, u)
	}
	if u.UUID != uuid || len(u.Keys) != 3 || u.PagesMissing {
		t.Fatalf("bad unit: %+v", u)
	}
	if u.Keys[0] != "essay/u/page_1.jpg" || u.Keys[2] != "essay/u/page_3.jpg" {
		t.Fatalf("order wrong: %v", u.Keys)
	}
	// 状态已清理
	if status, _ := b.Status(ctx, uuid); status != "expected=0 arrived=0 pages=0" {
		t.Fatalf("state not cleaned: %s", status)
	}
}

func TestEssayBatchOutOfOrder(t *testing.T) {
	rdb := testRedis(t)
	b := classify.NewEssayBatcher(rdb, time.Minute)
	ctx := context.Background()
	uuid := "test-uuid-ooo"
	cleanupKeys(t, rdb, uuid)
	exp := []int{1, 2, 3}

	// 乱序到达：3,1,2
	b.AddPage(ctx, uuid, 3, exp, "k3")
	b.AddPage(ctx, uuid, 1, exp, "k1")
	u, _ := b.AddPage(ctx, uuid, 2, exp, "k2")
	if u == nil || len(u.Keys) != 3 {
		t.Fatalf("should flush at 3/3, got %+v", u)
	}
	// 乱序到达但按物理页组装
	if u.Keys[0] != "k1" || u.Keys[1] != "k2" || u.Keys[2] != "k3" {
		t.Fatalf("order wrong: %v", u.Keys)
	}
}

func TestEssayBatchLateExpected(t *testing.T) {
	rdb := testRedis(t)
	b := classify.NewEssayBatcher(rdb, time.Minute)
	ctx := context.Background()
	uuid := "test-uuid-late"
	cleanupKeys(t, rdb, uuid)

	// 模板未就绪（expected 为空）：只记页，不判齐
	u, _ := b.AddPage(ctx, uuid, 1, nil, "k1")
	if u != nil {
		t.Fatal("expected 未登记就发了？")
	}
	// 后到的页带上模板集合，补登后判齐
	u, _ = b.AddPage(ctx, uuid, 2, []int{1, 2}, "k2")
	if u == nil || len(u.Keys) != 2 {
		t.Fatalf("should flush at 2/2, got %+v", u)
	}
}

func TestEssayBatchSweepTimeout(t *testing.T) {
	rdb := testRedis(t)
	b := classify.NewEssayBatcher(rdb, time.Nanosecond) // 立即超时
	ctx := context.Background()
	uuid := "test-uuid-sweep"
	cleanupKeys(t, rdb, uuid)

	b.AddPage(ctx, uuid, 1, []int{1, 2}, "k1")
	time.Sleep(2 * time.Millisecond)
	flushed, err := b.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 1 || flushed[0].UUID != uuid || !flushed[0].PagesMissing {
		t.Fatalf("sweep should flush incomplete with missing flag: %+v", flushed)
	}
}
