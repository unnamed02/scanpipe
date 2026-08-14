// Package tests 是 scanpipe 的黑盒测试集：只测导出 API，不碰内部实现。
// 内部实现的测试（未导出函数）留在各包内的 internal_test.go。
package tests

import (
	"encoding/base64"
	"os"
	"testing"

	"scanpipe/internal/classify"
)

func TestParseQRData(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		want    *classify.QRInfo
		wantErr bool
	}{
		{"5段gradeClass", "99,1,00139,6,G", &classify.QRInfo{
			Version: "99", CopyNumber: 1, PaperID: "00139", PageNumber: 6,
			ClassType: "gradeClass", Status: "ok"}, false},
		{"5段courseClass", "99,2,00139,3,C", &classify.QRInfo{
			Version: "99", CopyNumber: 2, PaperID: "00139", PageNumber: 3,
			ClassType: "courseClass", Status: "ok"}, false},
		{"4段默认course", "7,12,555,2", &classify.QRInfo{
			Version: "7", CopyNumber: 12, PaperID: "555", PageNumber: 2,
			ClassType: "courseClass", Status: "ok"}, false},
		{"3段旧格式", "8,666,1", &classify.QRInfo{
			CopyNumber: 8, PaperID: "666", PageNumber: 1,
			ClassType: "courseClass", Status: "ok"}, false},
		{"带空格", " 99 , 1 , 00139 , 6 , G ", &classify.QRInfo{
			Version: "99", CopyNumber: 1, PaperID: "00139", PageNumber: 6,
			ClassType: "gradeClass", Status: "ok"}, false},
		{"段数错误", "1,2", nil, true},
		{"非数字页码", "99,1,00139,x,G", nil, true},
		{"空串", "", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := classify.ParseQRData(c.text)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expect error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *got != *c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestBucketFor(t *testing.T) {
	cases := []struct {
		name    string
		info    *classify.ClassInfo
		paperID string
		want    string
	}{
		{"schoolClassId优先", &classify.ClassInfo{SchoolClassID: []int64{456}, ClassIDs: []int64{123}}, "00139", "class-456"},
		{"classIds次之", &classify.ClassInfo{ClassIDs: []int64{123}}, "00139", "class-123"},
		{"无班级信息降级paper", nil, "00139", "paper-00139"},
		{"全空兜底", nil, "", "quarantine"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classify.BucketFor(c.info, c.paperID); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestKeyBase(t *testing.T) {
	if got := classify.KeyBase("00139", "1", 6); got != "paper/00139/1/page_6" {
		t.Fatalf("got %s", got)
	}
}

func TestDecodeQRTextRealSamples(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"testdata/page_1.jpg", "99,1,00139,6,G"},
		{"testdata/page_2.jpg", "99,1,00139,5,G"},
	}
	for _, c := range cases {
		data, err := os.ReadFile(c.file)
		if err != nil {
			t.Skipf("sample %s not found", c.file)
		}
		got, err := classify.DecodeQRText(data)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if got != c.want {
			t.Fatalf("%s: got %q, want %q", c.file, got, c.want)
		}
	}
}

// 确保 base64 工具链可用（样例消息构造用）
func TestBase64RoundTrip(t *testing.T) {
	raw := []byte("hello")
	if back, _ := base64.StdEncoding.DecodeString(base64.StdEncoding.EncodeToString(raw)); string(back) != "hello" {
		t.Fatal("base64 broken")
	}
}
