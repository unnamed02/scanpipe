// qrpack.go 二维码打包格式（纯数字，56 bit → 17 位定长十进制）。
// 规范见 docs/QR_FORMAT.md（以文档为准）。
//
// 位布局（MSB → LSB，读取顺序即字段顺序）：
//
//	bit 55     位置 (1)        0=右上，1=左下
//	bit 48-54  份数 (7)        0-127 = studentid
//	bit 35-47  学校 (13)       0-8191
//	bit 34     班级类型 (1)    0=兴趣班，1=行政班
//	bit 15-33  班号区 (19)     按班级类型分支解读：
//	             行政班: bit 27-33 级数(7, 一年级入学年份后两位，如 2026 级=26) | bit 20-26 班号(7) | bit 15-19 科目(5, 码表A)
//	             兴趣班: bit 26-33 班号(8) | bit 15-25 科目(11)
//	bit 5-14   试卷编号 (10)   0-1023
//	bit 0-4    页码 (5)        0-31（物理页码）
package classify

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	qrShiftPosition = 55
	qrShiftCopy     = 48
	qrShiftSchool   = 35
	qrShiftClassTp  = 34
	qrShiftRegion   = 15
	qrShiftPaperID  = 5
	qrShiftPage     = 0
	qrPackedBits    = 56
	// QRPackedDigits QR 内容固定为 17 位十进制（左补零）。
	QRPackedDigits = 17
)

// 班号区（19 bit）内部偏移：行政班（相对区域低位）
const (
	qrRegSubjAShift    = 0 // 科目 bit 15-19（区域 bit 0-4）
	qrRegClassNoShift  = 5 // 班号 bit 20-26（区域 bit 5-11）
	qrRegGradeShift    = 12
	SubjectEssayAdmin = 31
)

// QRPacked 是打包格式的字段集合（班号区已按分支解好）。
type QRPacked struct {
	Position  int `json:"position"`   // 0=右上，1=左下
	Copy      int `json:"copy"`       // 0-127
	School    int `json:"school"`     // 0-8191
	ClassType int `json:"class_type"` // 0=兴趣班，1=行政班
	Grade     int `json:"grade"`      // 级数：一年级入学年份后两位（行政班 0-127；兴趣班 0）
	ClassNo   int `json:"class_no"`   // 行政班 0-127 / 兴趣班 0-255
	Subject   int `json:"subject"`    // 行政班 0-31 / 兴趣班 0-2047
	PaperID   int `json:"paper_id"`   // 0-1023
	Page      int `json:"page"`       // 0-31
}

// IsEssay 是否作文（仅行政班：科目=31；兴趣班作文码暂未约定，一律 false）。
func (p *QRPacked) IsEssay() bool {
	return p.ClassType == 1 && p.Subject == SubjectEssayAdmin
}

// UniqueID 五元组（学校+班级类型+级数+班号+科目+试卷编号，43 bit）。
func (p *QRPacked) UniqueID() uint64 {
	v, _ := PackQR(p)
	return (v >> qrShiftPaperID) & ((1 << 43) - 1)
}

// PackQR 打包为 uint64（56 bit）。
func PackQR(p *QRPacked) (uint64, error) {
	for _, chk := range []struct {
		name string
		v    int
		bits int
	}{
		{"position", p.Position, 1}, {"copy", p.Copy, 7},
		{"school", p.School, 13}, {"class_type", p.ClassType, 1},
		{"paper_id", p.PaperID, 10}, {"page", p.Page, 5},
	} {
		if chk.v < 0 || chk.v >= 1<<chk.bits {
			return 0, fmt.Errorf("%s out of range [0,%d): %d", chk.name, 1<<chk.bits, chk.v)
		}
	}
	var region uint64
	if p.ClassType == 1 {
		// 行政班：级数7 | 班号7 | 科目5
		if p.Grade < 0 || p.Grade >= 128 {
			return 0, fmt.Errorf("grade out of range [0,128): %d", p.Grade)
		}
		if p.ClassNo < 0 || p.ClassNo >= 128 {
			return 0, fmt.Errorf("class_no out of range [0,128): %d", p.ClassNo)
		}
		if p.Subject < 0 || p.Subject >= 32 {
			return 0, fmt.Errorf("subject out of range [0,32): %d", p.Subject)
		}
		region = uint64(p.Grade)<<qrRegGradeShift |
			uint64(p.ClassNo)<<qrRegClassNoShift |
			uint64(p.Subject)<<qrRegSubjAShift
	} else {
		// 兴趣班：班号(区域高8位) | 科目(区域低11位)
		if p.Grade != 0 {
			return 0, fmt.Errorf("interest class must have grade=0, got %d", p.Grade)
		}
		if p.ClassNo < 0 || p.ClassNo >= 256 {
			return 0, fmt.Errorf("class_no out of range [0,256): %d", p.ClassNo)
		}
		if p.Subject < 0 || p.Subject >= 2048 {
			return 0, fmt.Errorf("subject out of range [0,2048): %d", p.Subject)
		}
		region = uint64(p.ClassNo)<<11 | uint64(p.Subject)
	}
	v := uint64(p.Position) << qrShiftPosition
	v |= uint64(p.Copy) << qrShiftCopy
	v |= uint64(p.School) << qrShiftSchool
	v |= uint64(p.ClassType) << qrShiftClassTp
	v |= region << qrShiftRegion
	v |= uint64(p.PaperID) << qrShiftPaperID
	v |= uint64(p.Page) << qrShiftPage
	return v, nil
}

// UnpackQR 解包 uint64 为字段集合。
func UnpackQR(v uint64) (*QRPacked, error) {
	if v>>qrPackedBits != 0 {
		return nil, fmt.Errorf("packed value exceeds %d bits", qrPackedBits)
	}
	p := &QRPacked{
		Position:  int((v >> qrShiftPosition) & 1),
		Copy:      int((v >> qrShiftCopy) & ((1 << 7) - 1)),
		School:    int((v >> qrShiftSchool) & ((1 << 13) - 1)),
		ClassType: int((v >> qrShiftClassTp) & 1),
		PaperID:   int((v >> qrShiftPaperID) & ((1 << 10) - 1)),
		Page:      int((v >> qrShiftPage) & ((1 << 5) - 1)),
	}
	region := (v >> qrShiftRegion) & 0x7FFFF
	if p.ClassType == 1 {
		p.Grade = int((region >> qrRegGradeShift) & 0x7F)
		p.ClassNo = int((region >> qrRegClassNoShift) & 0x7F)
		p.Subject = int((region >> qrRegSubjAShift) & 0x1F)
	} else {
		p.ClassNo = int((region >> 11) & 0xFF)
		p.Subject = int(region & 0x7FF)
	}
	return p, nil
}

// FormatQR 把打包值格式化为 17 位定长十进制字符串（左补零）。
func FormatQR(v uint64) string {
	return fmt.Sprintf("%0*d", QRPackedDigits, v)
}

// ParseQR 统一入口：含逗号 → 老格式（3/4/5 段字符串）；
// 纯数字（≤17 位）→ 打包格式（56 bit）。
func ParseQR(text string) (*QRPacked, error) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, ",") {
		info, err := ParseQRData(text)
		if err != nil {
			return nil, err
		}
		return packedFromLegacy(info), nil
	}
	if text == "" {
		return nil, fmt.Errorf("empty qr")
	}
	if len(text) > QRPackedDigits {
		return nil, fmt.Errorf("packed qr exceeds %d digits", QRPackedDigits)
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("not a packed qr number: %q", text)
		}
	}
	v, err := strconv.ParseUint(text, 10, qrPackedBits)
	if err != nil {
		return nil, fmt.Errorf("packed qr overflow: %w", err)
	}
	return UnpackQR(v)
}

// packedFromLegacy 把老格式解析结果映射到打包字段集
// （老格式没有学校/级数/班号/科目/位置，填 0；班级类型按行政班处理）。
func packedFromLegacy(info *QRInfo) *QRPacked {
	classType := 0
	if info.ClassType == "gradeClass" || info.ClassType == "G" {
		classType = 1
	}
	paperID, _ := strconv.Atoi(info.PaperID)
	return &QRPacked{
		Copy:      info.CopyNumber,
		ClassType: classType,
		PaperID:   paperID,
		Page:      info.PageNumber,
	}
}

// ToQRInfo 转成现有 QRInfo（兼容老代码路径；打包扩展字段丢失）。
func (p *QRPacked) ToQRInfo() *QRInfo {
	classType := "courseClass"
	if p.ClassType == 1 {
		classType = "gradeClass"
	}
	pageKind := ""
	if p.IsEssay() {
		pageKind = "W"
	}
	return &QRInfo{
		CopyNumber: p.Copy,
		PaperID:    strconv.Itoa(p.PaperID),
		PageNumber: p.Page,
		ClassType:  classType,
		PageKind:   pageKind,
		Status:     "ok",
	}
}
