package classify

// 本文件只放必须触碰未导出实现的测试。
// 导出 API 的黑盒测试统一在 scanpipe/tests/ 目录。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"os"
	"testing"
)

// route 的隔离路径：坏消息全部进 quarantine，不panic、不报ok=false。
func TestRouteQuarantine(t *testing.T) {
	c := &Classifier{}
	ctx := context.Background()

	cases := []struct {
		name    string
		payload string
	}{
		{"非JSON", "not-json"},
		{"坏base64", `{"image":"!!!"}`},
		{"非图片", `{"image":"` + base64.StdEncoding.EncodeToString([]byte("hello")) + `"}`},
	}
	for _, c2 := range cases {
		t.Run(c2.name, func(t *testing.T) {
			target, out, ok, _ := c.route(ctx, []byte(c2.payload), "quarantine")
			if target != "quarantine" {
				t.Fatalf("target=%s, want quarantine", target)
			}
			if !ok {
				t.Fatal("quarantine 路径不应返回 ok=false（应正常ACK）")
			}
			var doc map[string]any
			if json.Unmarshal(out, &doc) != nil {
				t.Fatal("quarantine 消息不是合法JSON")
			}
		})
	}
}


// ---- benchmarks（go test -bench=. -benchtime=20x ./internal/classify/）----

func loadSample(b *testing.B, rel string) []byte {
	data, err := os.ReadFile(rel)
	if err != nil {
		b.Skipf("sample %s not found", rel)
	}
	return data
}

// 全链路基准
func BenchmarkDecodeQRText(b *testing.B) {
	for _, f := range []string{"../../tests/testdata/page_1.jpg", "../../tests/testdata/page_2.jpg"} {
		data := loadSample(b, f)
		b.Run(f, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				DecodeQRText(data)
			}
		})
	}
}

// 分解成本：jpeg解码 / 灰度视图 / 各候选级（含命中与否）
func BenchmarkStages(b *testing.B) {
	data := loadSample(b, "../../tests/testdata/page_1.jpg")
	img, _, _ := image.Decode(bytes.NewReader(data))
	gray := grayView(img)
	gb := gray.Bounds()
	w, h := gb.Dx(), gb.Dy()

	candidates := []struct {
		name string
		img  image.Image
	}{
		{"corner", subImage(gray, image.Rect(w*70/100, 0, w, h*30/100))},
		{"roi", subImage(gray, image.Rect(w*55/100, 0, w, h*45/100))},
		{"corner2x", scale2x(subImage(gray, image.Rect(w*70/100, 0, w, h*30/100)))},
		{"topHalf", subImage(gray, image.Rect(0, 0, w, h*55/100))},
		{"full", gray},
	}

	b.Run("jpeg_decode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			image.Decode(bytes.NewReader(data))
		}
	})
	for _, c := range candidates {
		b.Run("candidate/"+c.name, func(b *testing.B) {
			var hits int
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := decodeImage(c.img); err == nil {
					hits++
				}
			}
			b.ReportMetric(float64(hits)*100/float64(b.N), "hit%")
		})
	}
}
