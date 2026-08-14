// paper.go 试卷数据客户端：从 hyxq 拉取完整试卷模板（题目坐标/答案/判题标准），
// 磁盘+内存两级缓存。对齐 mycorrect1.4 core/server_manager.py 的真实接口结构：
//
//	GET /qedu-api/qedu/schoolteacher/xkw/getTeacherExamPapers?paperId=          (courseClass)
//	GET /qedu-api/qedu/schoolteacher/xkw/getSchoolClassExamPaperWithCoordinatesAdmin?paperId= (gradeClass)
//
//	响应: {"code":200, "data":[{"jsonData":"<内嵌JSON字符串>",
//	       "createDate":"...","updateDate":"...","deleted":0}, ...]}
//	jsonData 解析后 = {"version":"99","paperList":[...]} 或多版本列表
package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Paper 是一份完整试卷模板。
type Paper struct {
	Raw     map[string]any `json:"raw"`     // 完整文档（含 paperList）
	Version string         `json:"version"` // 实际匹配到的版本
	Source  string         `json:"source"`  // courseClass | gradeClass
}

// ClassInfo 从试卷文档提取的班级信息（root_info 子集）。
type ClassInfo struct {
	ClassName     string  `json:"className"`
	Subject       string  `json:"subject"`
	PaperType     string  `json:"paperType"`
	TeacherName   string  `json:"teacherName"`
	TeacherSchool string  `json:"teacherSchool"`
	ClassIDs      []int64 `json:"classIds"`
	SchoolClassID []int64 `json:"schoolClassId"`
	Source        string  `json:"source"`
}

// PageKind 页面类型（对齐 mycorrect1.4 analyze_page_paper_type 的语义）。
type PageKind int

const (
	PageNormal PageKind = iota // 常规题页
	PageEssay                  // 纯作文页
	PageMixed                  // 混合页（作文 + 常规题）
)

// PageKindOf 判断指定物理页的类型：typeName 含"写作"或"作文"子串即为作文大题。
// 与原项目 analyze_page_paper_type 完全一致（子串判定，不维护题型清单）。
func (p *Paper) PageKindOf(pageNumber int) PageKind {
	hasEssay, hasNormal := false, false
	paperList, _ := p.Raw["paperList"].([]any)
	for _, pv := range paperList {
		pageObj, ok := pv.(map[string]any)
		if !ok || intFromAny(pageObj["page"]) != pageNumber {
			continue
		}
		sections, _ := pageObj["sections"].([]any)
		for _, sv := range sections {
			section, ok := sv.(map[string]any)
			if !ok {
				continue
			}
			typeName, _ := section["typeName"].(string)
			if strings.Contains(typeName, "写作") || strings.Contains(typeName, "作文") {
				hasEssay = true
			} else {
				hasNormal = true
			}
		}
		break // 与原实现一致：只看本页第一个 pageObj
	}
	switch {
	case hasEssay && hasNormal:
		return PageMixed
	case hasEssay:
		return PageEssay
	default:
		return PageNormal
	}
}

// EssayPages 返回试卷中的作文物理页列表（升序；混合页也算作文页）。
// 无作文页时返回空。
func (p *Paper) EssayPages() []int {
	var out []int
	paperList, _ := p.Raw["paperList"].([]any)
	for _, pv := range paperList {
		pageObj, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		if n := intFromAny(pageObj["page"]); n > 0 && p.PageKindOf(n) != PageNormal {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		var i int
		fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}

// WriteAnswersToRedis 已被移除（2026-07 设计回退：判题标准由批改 worker 自取）。

// ClassInfo 提取班级信息字段。
func (p *Paper) ClassInfo() *ClassInfo {
	str := func(k string) string {
		v, _ := p.Raw[k].(string)
		return v
	}
	ids := func(k string) []int64 {
		var out []int64
		if arr, ok := p.Raw[k].([]any); ok {
			for _, v := range arr {
				if f, ok := v.(float64); ok {
					out = append(out, int64(f))
				}
			}
		}
		return out
	}
	return &ClassInfo{
		ClassName:     str("className"),
		Subject:       str("subject"),
		PaperType:     str("paperType"),
		TeacherName:   str("teacherName"),
		TeacherSchool: str("teacherSchool"),
		ClassIDs:      ids("classIds"),
		SchoolClassID: ids("schoolClassId"),
		Source:        p.Source,
	}
}

// PaperClient 拉取并缓存试卷。三级查找：内存 → 磁盘缓存 → API。
type PaperClient struct {
	BaseURL  string
	Token    string
	CacheDir string // 空 = 只内存缓存

	mu   sync.Mutex
	mem  map[string]*Paper
	hc   *http.Client
}

func NewPaperClient(baseURL, token, cacheDir string) *PaperClient {
	return &PaperClient{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Token:    token,
		CacheDir: cacheDir,
		mem:      make(map[string]*Paper),
		hc:       &http.Client{Timeout: 15 * time.Second},
	}
}

const (
	courseAPI = "/qedu-api/qedu/schoolteacher/xkw/getTeacherExamPapers"
	gradeAPI  = "/qedu-api/qedu/schoolteacher/xkw/getSchoolClassExamPaperWithCoordinatesAdmin"
)

func (c *PaperClient) cacheKey(paperID, version, classType string) string {
	return paperID + "|" + version + "|" + classType
}

func (c *PaperClient) cachePath(paperID, version, classType string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == ':' {
				return '_'
			}
			return r
		}, s)
	}
	return filepath.Join(c.CacheDir,
		fmt.Sprintf("%s_%s_%s.json", clean(paperID), clean(version), clean(classType)))
}

// Get 获取试卷模板。version 为空 = 最新版本。
func (c *PaperClient) Get(ctx context.Context, paperID, version, classType string) (*Paper, error) {
	key := c.cacheKey(paperID, version, classType)
	c.mu.Lock()
	if p, ok := c.mem[key]; ok {
		c.mu.Unlock()
		return p, nil
	}
	c.mu.Unlock()

	// 磁盘缓存
	if c.CacheDir != "" {
		if p, err := c.readDiskCache(c.cachePath(paperID, version, classType)); err == nil {
			c.mu.Lock()
			c.mem[key] = p
			c.mu.Unlock()
			return p, nil
		}
	}

	// API：主接口失败回退备接口（对齐 get_exam_papers）
	primary, fallback := courseAPI, gradeAPI
	source := "courseClass"
	if classType == "gradeClass" {
		primary, fallback = fallback, primary
		source = "gradeClass"
	}
	paper, err := c.fetchPaper(ctx, primary, paperID, version, source)
	if err != nil {
		altSource := "courseClass"
		if source == "courseClass" {
			altSource = "gradeClass"
		}
		paper, err = c.fetchPaper(ctx, fallback, paperID, version, altSource)
	}
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.mem[key] = paper
	c.mu.Unlock()
	if c.CacheDir != "" {
		c.writeDiskCache(c.cachePath(paperID, version, classType), paper)
	}
	return paper, nil
}

// fetchPaper 调一个接口并解析出试卷文档。
func (c *PaperClient) fetchPaper(ctx context.Context, path, paperID, version, source string) (*Paper, error) {
	url := fmt.Sprintf("%s%s?paperId=%s", c.BaseURL, path, paperID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", c.Token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("bad envelope: %w", err)
	}
	if envelope.Code != 200 {
		return nil, fmt.Errorf("api code=%d msg=%s", envelope.Code, envelope.Msg)
	}
	var records []struct {
		JsonData   string `json:"jsonData"`
		CreateDate string `json:"createDate"`
		UpdateDate string `json:"updateDate"`
		Deleted    int    `json:"deleted"`
	}
	if err := json.Unmarshal(envelope.Data, &records); err != nil {
		return nil, fmt.Errorf("bad records: %w", err)
	}
	return selectPaper(recordsToRaw(records), version, source)
}

// rawRecord 是解析中间态。
type rawRecord struct {
	jsonData   string
	createDate string
	updateDate string
}

func recordsToRaw(records []struct {
	JsonData   string `json:"jsonData"`
	CreateDate string `json:"createDate"`
	UpdateDate string `json:"updateDate"`
	Deleted    int    `json:"deleted"`
}) []rawRecord {
	var out []rawRecord
	for _, r := range records {
		if r.Deleted == 1 || r.JsonData == "" {
			continue
		}
		out = append(out, rawRecord{jsonData: r.JsonData, createDate: r.CreateDate, updateDate: r.UpdateDate})
	}
	return out
}

// selectPaper 从记录中选出试卷文档：version 非空按版本匹配（找不到回退最新），
// 否则取 createDate/updateDate 最新一条。文档必须含 paperList。
func selectPaper(records []rawRecord, version, source string) (*Paper, error) {
	type candidate struct {
		doc     map[string]any
		version string
		date    string
	}
	var cands []candidate
	for _, r := range records {
		for _, doc := range parseJsonData(r.jsonData) {
			if _, ok := doc["paperList"]; !ok {
				continue
			}
			v, _ := doc["version"].(string)
			date := r.createDate
			if r.updateDate > date {
				date = r.updateDate
			}
			cands = append(cands, candidate{doc: doc, version: v, date: date})
		}
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no valid paper records (need paperList)")
	}
	// 版本匹配
	if version != "" {
		for _, cd := range cands {
			if cd.version == version {
				return &Paper{Raw: cd.doc, Version: cd.version, Source: source}, nil
			}
		}
		// 回退最新（对齐原项目的 version fallback 行为）
	}
	latest := cands[0]
	for _, cd := range cands[1:] {
		if cd.date > latest.date {
			latest = cd
		}
	}
	return &Paper{Raw: latest.doc, Version: latest.version, Source: source}, nil
}

// parseJsonData 解析内嵌 jsonData 字符串：可能是对象或对象列表。
func parseJsonData(s string) []map[string]any {
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err == nil {
		return []map[string]any{doc}
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr
	}
	return nil
}

func (c *PaperClient) readDiskCache(path string) (*Paper, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Paper
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *PaperClient) writeDiskCache(path string, p *Paper) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0o644)
}
