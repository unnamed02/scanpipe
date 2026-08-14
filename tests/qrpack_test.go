package tests

import (
	"strconv"
	"testing"

	"scanpipe/internal/classify"
)

func TestQRPackRoundTripAdmin(t *testing.T) {
	p := &classify.QRPacked{
		Position: 0, Copy: 1, School: 88, ClassType: 1,
		Grade: 23, ClassNo: 2, Subject: 0, PaperID: 139, Page: 6,
	}
	v, err := classify.PackQR(p)
	if err != nil {
		t.Fatal(err)
	}
	s := classify.FormatQR(v)
	if len(s) != 17 {
		t.Fatalf("want 17 digits, got %d", len(s))
	}
	// 文档样例值
	if s != "00284518902665574" {
		t.Fatalf("doc sample mismatch: %s", s)
	}
	back, err := classify.UnpackQR(v)
	if err != nil || *back != *p {
		t.Fatalf("roundtrip: %v got %+v", err, back)
	}
	// 五元组 43 bit
	if pid := p.UniqueID(); pid != 95122686091 {
		t.Fatalf("UniqueID mismatch: %d", pid)
	}
}

func TestQRPackRoundTripInterest(t *testing.T) {
	p := &classify.QRPacked{
		Position: 1, Copy: 99, School: 88, ClassType: 0,
		ClassNo: 12, Subject: 42, PaperID: 56, Page: 3,
	}
	v, err := classify.PackQR(p)
	if err != nil {
		t.Fatal(err)
	}
	s := classify.FormatQR(v)
	if s != "63897844176979715" {
		t.Fatalf("interest sample mismatch: %s", s)
	}
	back, err := classify.UnpackQR(v)
	if err != nil || *back != *p {
		t.Fatalf("roundtrip: %v got %+v", err, back)
	}
	if pid := p.UniqueID(); pid != 94514489400 {
		t.Fatalf("UniqueID mismatch: %d", pid)
	}
}

func TestQRPackEssayFlag(t *testing.T) {
	admin := &classify.QRPacked{ClassType: 1, Subject: classify.SubjectEssayAdmin}
	if !admin.IsEssay() {
		t.Fatal("admin subject=31 should be essay")
	}
	// 兴趣班作文码未约定，任何科目都不应判为作文
	interest := &classify.QRPacked{ClassType: 0, Subject: 2047}
	if interest.IsEssay() {
		t.Fatal("interest essay code undefined; should not be essay")
	}
	normal := &classify.QRPacked{ClassType: 1, Subject: 30}
	if normal.IsEssay() {
		t.Fatal("subject=30 should not be essay")
	}
}

func TestQRPackMaxValues(t *testing.T) {
	p := &classify.QRPacked{
		Position: 1, Copy: 127, School: 8191, ClassType: 1,
		Grade: 127, ClassNo: 127, Subject: 31, PaperID: 1023, Page: 31,
	}
	v, err := classify.PackQR(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(classify.FormatQR(v)) > 17 {
		t.Fatalf("max overflows 17 digits: %s", classify.FormatQR(v))
	}
	if _, err := classify.UnpackQR(v); err != nil {
		t.Fatal(err)
	}
	// 兴趣班极值
	p2 := &classify.QRPacked{
		Position: 1, Copy: 127, School: 8191, ClassType: 0,
		ClassNo: 255, Subject: 2047, PaperID: 1023, Page: 31,
	}
	v2, err := classify.PackQR(p2)
	if err != nil {
		t.Fatal(err)
	}
	if len(classify.FormatQR(v2)) > 17 {
		t.Fatalf("interest max overflows 17 digits: %s", classify.FormatQR(v2))
	}
}

func TestQRPackRangeValidation(t *testing.T) {
	cases := []*classify.QRPacked{
		{Copy: 128},                     // 份数上限 127
		{PaperID: 1024},                 // 试卷编号上限 1023
		{School: 8192},                  // 学校上限 8191
		{Page: 32},                      // 页码上限 31
		{ClassType: 1, Grade: 128},      // 级数上限 127
		{ClassType: 1, ClassNo: 128},    // 行政班班号上限 127
		{ClassType: 1, Subject: 32},     // 行政班科目上限 31
		{ClassType: 0, ClassNo: 256},    // 兴趣班班号上限 255
		{ClassType: 0, Subject: 2048},   // 兴趣班科目上限 2047
		{ClassType: 0, Grade: 1},        // 兴趣班不允许级数
	}
	for i, bad := range cases {
		if _, err := classify.PackQR(bad); err == nil {
			t.Fatalf("case %d should be rejected: %+v", i, bad)
		}
	}
}

func TestParseQRDispatches(t *testing.T) {
	// 老格式（逗号）
	p, err := classify.ParseQR("99,1,00139,6,G")
	if err != nil {
		t.Fatal(err)
	}
	if p.PaperID != 139 || p.Page != 6 || p.ClassType != 1 || p.Copy != 1 {
		t.Fatalf("legacy parse wrong: %+v", p)
	}

	// 打包格式（17 位定长）
	want := &classify.QRPacked{Copy: 1, ClassType: 1, Grade: 23, ClassNo: 2, Subject: 0, PaperID: 139, Page: 6}
	v, _ := classify.PackQR(want)
	p2, err := classify.ParseQR(classify.FormatQR(v))
	if err != nil {
		t.Fatal(err)
	}
	if *p2 != *want {
		t.Fatalf("packed parse wrong: got %+v, want %+v", p2, want)
	}

	// 非法输入
	for _, bad := range []string{"12ab", "", "123456789012345678"} {
		if _, err := classify.ParseQR(bad); err == nil {
			t.Fatalf("should reject %q", bad)
		}
	}
	// 超 56 bit
	if _, err := classify.ParseQR("72057594037927937"); err == nil {
		t.Fatal("should reject >56bit value")
	}
}

func TestQRPackedToQRInfo(t *testing.T) {
	p := &classify.QRPacked{Copy: 3, ClassType: 1, Subject: 31, PaperID: 139, Page: 6}
	info := p.ToQRInfo()
	if info.CopyNumber != 3 || info.PaperID != "139" || info.PageNumber != 6 ||
		info.ClassType != "gradeClass" || info.PageKind != "W" || info.Status != "ok" {
		t.Fatalf("ToQRInfo wrong: %+v", info)
	}
	normal := &classify.QRPacked{ClassType: 1, Subject: 0}
	if normal.ToQRInfo().PageKind != "" {
		t.Fatal("non-essay should have empty PageKind")
	}
}

func TestParseQRUintParse(t *testing.T) {
	if _, err := strconv.Atoi("17"); err != nil {
		t.Fatal(err)
	}
}
