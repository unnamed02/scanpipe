package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"scanpipe/internal/classify"
)

// mockHyxq 模拟 hyxq 试卷接口：返回 records[{jsonData, createDate, deleted}] 结构。
func mockHyxq(t *testing.T, records []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"code": 200, "msg": "ok", "data": records}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestPaperClientGet(t *testing.T) {
	paperV1, _ := json.Marshal(map[string]any{"version": "1", "paperList": []any{1}, "className": "一班"})
	paperV2, _ := json.Marshal(map[string]any{"version": "2", "paperList": []any{2}, "className": "二班"})

	records := []map[string]any{
		{"jsonData": string(paperV1), "createDate": "2026-07-01 10:00:00", "deleted": 0},
		{"jsonData": string(paperV2), "createDate": "2026-07-02 10:00:00", "deleted": 0},
	}

	srv := mockHyxq(t, records)
	defer srv.Close()

	cacheDir := t.TempDir()
	client := classify.NewPaperClient(srv.URL, "token", cacheDir)

	t.Run("默认取最新版本", func(t *testing.T) {
		p, err := client.Get(context.Background(), "00139", "", "courseClass")
		if err != nil {
			t.Fatal(err)
		}
		if p.Version != "2" || p.Raw["className"] != "二班" {
			t.Fatalf("got version=%s class=%v", p.Version, p.Raw["className"])
		}
		if p.Source != "courseClass" {
			t.Fatalf("source=%s", p.Source)
		}
	})

	t.Run("按版本匹配", func(t *testing.T) {
		p, err := client.Get(context.Background(), "00139", "1", "courseClass")
		if err != nil {
			t.Fatal(err)
		}
		if p.Version != "1" {
			t.Fatalf("got version=%s", p.Version)
		}
	})

	t.Run("内存缓存命中", func(t *testing.T) {
		// 第二次 Get 不再打 HTTP（mock 关了也能拿到）
		p, err := client.Get(context.Background(), "00139", "", "courseClass")
		if err != nil || p.Version != "2" {
			t.Fatalf("cache miss: %v", err)
		}
	})

	t.Run("ClassInfo提取", func(t *testing.T) {
		p, _ := client.Get(context.Background(), "00139", "", "courseClass")
		ci := p.ClassInfo()
		if ci.ClassName != "二班" {
			t.Fatalf("got %s", ci.ClassName)
		}
	})
}

func TestPaperClientFallback(t *testing.T) {
	// 主接口 500 → 回退备接口
	paperV1, _ := json.Marshal(map[string]any{"version": "1", "paperList": []any{1}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/qedu-api/qedu/schoolteacher/xkw/getTeacherExamPapers" {
			json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "not found"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "data": []map[string]any{
				{"jsonData": string(paperV1), "createDate": "2026-07-01 10:00:00"},
			},
		})
	}))
	defer srv.Close()

	client := classify.NewPaperClient(srv.URL, "", t.TempDir())
	p, err := client.Get(context.Background(), "00139", "", "courseClass")
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != "gradeClass" {
		t.Fatalf("fallback failed, source=%s", p.Source)
	}
}

func TestPaperClientDiskCache(t *testing.T) {
	paperV1, _ := json.Marshal(map[string]any{"version": "1", "paperList": []any{1}})
	srv := mockHyxq(t, []map[string]any{
		{"jsonData": string(paperV1), "createDate": "2026-07-01 10:00:00"},
	})
	cacheDir := t.TempDir()

	c1 := classify.NewPaperClient(srv.URL, "", cacheDir)
	if _, err := c1.Get(context.Background(), "00139", "", "courseClass"); err != nil {
		t.Fatal(err)
	}
	srv.Close() // 关掉服务，第二个客户端应命中磁盘缓存

	c2 := classify.NewPaperClient(srv.URL, "", cacheDir)
	p, err := c2.Get(context.Background(), "00139", "", "courseClass")
	if err != nil {
		t.Fatalf("disk cache miss: %v", err)
	}
	if p.Version != "1" {
		t.Fatalf("got version=%s", p.Version)
	}
	fmt.Println("disk cache hit:", p.Version)
}

func TestPageKindOf(t *testing.T) {
	p := &classify.Paper{Raw: map[string]any{
		"paperList": []any{
			map[string]any{"page": 5, "sections": []any{
				map[string]any{"typeName": "命题作文"},
			}},
			map[string]any{"page": 6, "sections": []any{
				map[string]any{"typeName": "单选题"},
				map[string]any{"typeName": "微写作"},
			}},
			map[string]any{"page": 7, "sections": []any{
				map[string]any{"typeName": "单选题"},
			}},
		},
	}}
	if got := p.PageKindOf(5); got != classify.PageEssay {
		t.Fatalf("page5: got %v, want PageEssay", got)
	}
	if got := p.PageKindOf(6); got != classify.PageMixed {
		t.Fatalf("page6: got %v, want PageMixed", got)
	}
	if got := p.PageKindOf(7); got != classify.PageNormal {
		t.Fatalf("page7: got %v, want PageNormal", got)
	}
	if got := p.PageKindOf(99); got != classify.PageNormal {
		t.Fatalf("page99: got %v, want PageNormal", got)
	}
}
