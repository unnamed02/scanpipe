// Package classify 是 QR 分类工人：
// 从 Redis Streams 消费 raw_pages，解码试卷二维码，
// 常规题按 五元组(unique_id)+物理页码 分流到 paper_<unique_id>_p<page> 流，
// 作文页聚合整篇后发 essay_pages；识别失败进 quarantine。
//
// QR 内容格式（新老兼容，新格式见 docs/QR_FORMAT.md）：
//
//	打包: 17 位纯数字（56 bit，43 bit 五元组唯一标识一份试卷）
//	5 段: version,copy,paper_id,page,classType(C=courseClass/G=gradeClass)
//	4 段: version,copy,paper_id,page        （classType 默认 courseClass）
//	3 段: copy,paper_id,page                （旧格式，默认 courseClass）
package classify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"golang.org/x/image/draw"

	"scanpipe/internal/mqclient"
)

// QRInfo 是解析后的二维码字段。
type QRInfo struct {
	Version    string `json:"version"`
	CopyNumber int    `json:"copy_number"`
	PaperID    string `json:"paper_id"`
	PageNumber int    `json:"page_number"`
	ClassType  string `json:"class_type"`
	PageKind   string `json:"page_kind"` // 第六位页类型：W=作文页，空=未标定（回退模板判定）
	Status     string `json:"status"`    // ok | failed
}

// ParseQRData 解析 QR 文本为结构化字段，对齐 qr_detector.py 的三种格式。
func ParseQRData(text string) (*QRInfo, error) {
	parts := strings.Split(strings.TrimSpace(text), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	atoi := func(s string) (int, error) { return strconv.Atoi(s) }

	switch len(parts) {
	case 5:
		classType := parts[4]
		switch classType {
		case "C":
			classType = "courseClass"
		case "G":
			classType = "gradeClass"
		}
		copyNum, err1 := atoi(parts[1])
		pageNum, err2 := atoi(parts[3])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad 5-part qr: %v %v", err1, err2)
		}
		return &QRInfo{Version: parts[0], CopyNumber: copyNum, PaperID: parts[2],
			PageNumber: pageNum, ClassType: classType, Status: "ok"}, nil
	case 4:
		copyNum, err1 := atoi(parts[1])
		pageNum, err2 := atoi(parts[3])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad 4-part qr: %v %v", err1, err2)
		}
		return &QRInfo{Version: parts[0], CopyNumber: copyNum, PaperID: parts[2],
			PageNumber: pageNum, ClassType: "courseClass", Status: "ok"}, nil
	case 3:
		copyNum, err1 := atoi(parts[0])
		pageNum, err2 := atoi(parts[2])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad 3-part qr: %v %v", err1, err2)
		}
		return &QRInfo{CopyNumber: copyNum, PaperID: parts[1],
			PageNumber: pageNum, ClassType: "courseClass", Status: "ok"}, nil
	default:
		return nil, fmt.Errorf("unexpected %d parts", len(parts))
	}
}

// DecodeQRText 从 JPEG 字节中解码 QR 文本。
// 候选阶梯对齐 mycorrect1.4 core/qr_detector.py（区域优先 + 惰性求值）：
//  1. 右上 30%×30% 角区（QR 模板固定位置，最小像素量）
//  2. 右上 ROI（0.45h × 0.55w）
//  3. 角区 2x 放大
//  4. 上半区
//  5. 全图
//
// 性能要点：JPEG 的 Y 通道本身就是灰度图，直接零拷贝复用，
// 跳过 gozxing 二值化里最贵的 RGB→亮度换算。
func DecodeQRText(jpegData []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return "", fmt.Errorf("decode jpeg: %w", err)
	}
	gray := grayView(img)
	b := gray.Bounds()
	w, h := b.Dx(), b.Dy()

	// 惰性候选：每级解码成功即返回，绝不算用不到的候选
	if text, err := decodeImage(subImage(gray, image.Rect(w*70/100, 0, w, h*30/100))); err == nil {
		return text, nil
	}
	if text, err := decodeImage(subImage(gray, image.Rect(w*55/100, 0, w, h*45/100))); err == nil {
		return text, nil
	}
	if text, err := decodeImage(scale2x(subImage(gray, image.Rect(w*70/100, 0, w, h*30/100)))); err == nil {
		return text, nil
	}
	if text, err := decodeImage(subImage(gray, image.Rect(0, 0, w, h*55/100))); err == nil {
		return text, nil
	}
	if text, err := decodeImage(gray); err == nil {
		return text, nil
	}
	return "", fmt.Errorf("no qr found")
}

// subImage 裁剪图片（调用方需确保 img 支持 SubImage；Gray/YCbCr/RGBA 均支持）。
func subImage(img image.Image, r image.Rectangle) image.Image {
	return img.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(r)
}

// grayView 零拷贝取图片的灰度视图。
// JPEG 解出的 *image.YCbCr 的 Y 平面直接就是灰度，包一层 image.Gray 即可。
func grayView(img image.Image) image.Image {
	if ycbcr, ok := img.(*image.YCbCr); ok {
		return &image.Gray{Pix: ycbcr.Y, Stride: ycbcr.YStride, Rect: ycbcr.Rect}
	}
	b := img.Bounds()
	gray := image.NewGray(b)
	draw.Draw(gray, b, img, b.Min, draw.Src)
	return gray
}

func decodeImage(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	result, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", err
	}
	return result.GetText(), nil
}

func scale2x(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*2, b.Dy()*2))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// Classifier QR 分类工人：raw_pages → paper_<paper_id>_p<page> / essay_pages / quarantine。
type Classifier struct {
	SrcStream  string           // 默认 raw_pages
	Group      string           // 消费组名，默认 classifier
	Quarantine string           // 识别失败流，默认 quarantine
	ClassInfo  *PaperClient     // nil = 不拉试卷信息（纯 QR 路由，无作文判定）
	Archiver   *Archiver        // nil = 不落盘（消息内联 base64）
	Batcher    *EssayBatcher    // nil = 不聚合作文批次
	EssayStream string          // 作文流名，默认 essay_pages
}

func (c *Classifier) src() string {
	if c.SrcStream == "" {
		return "raw_pages"
	}
	return c.SrcStream
}

// Run 阻塞运行分类循环，直到 ctx 取消。
// mqAddr 格式 host:port；每个 worker 独立连接（BLOCK 不互相占用）。
// 连接出错自动重连（指数退避，上限 30s）。
func (c *Classifier) Run(ctx context.Context, mqAddr, mqPassword, worker string) error {
	group := c.Group
	if group == "" {
		group = "classifier"
	}
	var mq *mqclient.Client
	backoff := time.Second
	dial := func() error {
		if mq != nil {
			mq.Close()
		}
		var err error
		mq, err = mqclient.Dial(mqAddr, mqPassword)
		return err
	}
	if err := dial(); err != nil {
		return fmt.Errorf("dial mq: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			mq.Close()
			return nil
		default:
		}
		msgs, err := mq.Read(c.src(), group, worker, 1, 5000)
		if err != nil {
			time.Sleep(backoff)
			if derr := dial(); derr != nil {
				backoff = min(backoff*2, 30*time.Second)
			} else {
				backoff = time.Second
			}
			continue
		}
		if len(msgs) == 0 {
			continue // BLOCK 超时，空转
		}
		msg := msgs[0]
		target, payload, ok, unit := c.route(ctx, []byte(msg.Payload), c.quarantine())
		if !ok {
			continue // 落盘失败：不 ACK，等 PEL 超时重投
		}
		if unit != nil {
			// 作文批次到齐：整篇发出
			unitJSON, _ := json.Marshal(unit)
			if _, err := mq.Add(c.essayStream(), string(unitJSON)); err != nil {
				continue // 不 ACK，等重投
			}
		}
		if target != "" {
			if _, err := mq.Add(target, string(payload)); err != nil {
				continue // 不 ACK，等重投
			}
		}
		log.Printf("routed: id=%s → %s (essay_unit=%v)", msg.ID, target, unit != nil)
		mq.Ack(c.src(), group, msg.ID)
	}
}

func (c *Classifier) essayStream() string {
	if c.EssayStream == "" {
		return "essay_pages"
	}
	return c.EssayStream
}

func (c *Classifier) quarantine() string {
	if c.Quarantine == "" {
		return "quarantine"
	}
	return c.Quarantine
}

// route 决定目标流并注入 qr / class 字段，返回 (目标流, 新payload)。
// route 决定目标流并注入 qr / class 字段。
// 配置了 Archiver 时：原图 + 元数据落 RustFS（按班级分 bucket），
// 队列消息里的 image 替换为 image_ref。
// 返回值 ok=false 表示落盘失败，调用方不 ACK，等 PEL 重投。
func (c *Classifier) route(ctx context.Context, payload []byte, quarantine string) (string, []byte, bool, *EssayUnit) {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return quarantine, withQR(payload, &QRInfo{Status: "failed"}), true, nil
	}
	imgB64, _ := doc["image"].(string)
	jpegData, err := base64.StdEncoding.DecodeString(imgB64)
	if err != nil {
		return quarantine, withQR(payload, &QRInfo{Status: "failed"}), true, nil
	}
	text, err := DecodeQRText(jpegData)
	if err != nil {
		return quarantine, withQR(payload, &QRInfo{Status: "failed"}), true, nil
	}
	packed, err := ParseQR(text)
	if err != nil {
		return quarantine, withQR(payload, &QRInfo{Status: "failed"}), true, nil
	}
	log.Printf("qr decoded: %q (uid=%d copy=%d paper=%d page=%d)", text, packed.UniqueID(), packed.Copy, packed.PaperID, packed.Page)
	info := packed.ToQRInfo()
	out := withQR(payload, info)
	// 试卷模板：班级信息（尽力而为）+ 作文页判定（路由依据）
	var ci *ClassInfo
	var essayPages []int
	pageKind := PageNormal
	if c.ClassInfo != nil {
		if p, err := c.ClassInfo.Get(ctx, info.PaperID, info.Version, info.ClassType); err == nil {
			ci = p.ClassInfo()
			out = withClassInfo(out, ci)
			pageKind = p.PageKindOf(info.PageNumber)
			essayPages = p.EssayPages()
		}
	}
	if packed.IsEssay() {
		pageKind = PageEssay // QR 科目=作文：整卷皆作文页
	}

	// 落盘：只存原图到 class-<id> bucket
	// key 与 hyxq 云存储对齐：paper/<paper_id>/<studentid>/page_<n>.jpg
	imgKey := ""
	if c.Archiver != nil {
		bucket := BucketFor(ci, info.PaperID)
		studentID := strconv.Itoa(info.CopyNumber) // QR 第二位 = studentid
		imgKey = KeyBase(info.PaperID, studentID, info.PageNumber) + ".jpg"
		if err := c.Archiver.PutImage(ctx, bucket, imgKey, jpegData); err != nil {
			return "", nil, false, nil // 不 ACK，等重投
		}
		log.Printf("archived: %s/%s (%d bytes)", bucket, imgKey, len(jpegData))
		minimal, _ := json.Marshal(map[string]any{
			"copy_number": info.CopyNumber,
			"key":         imgKey,
		})
		out = minimal
	}

	// 作文页：进批次聚合（按物理页判齐），等齐了由 unit 发出
	var unit *EssayUnit
	if pageKind != PageNormal && c.Batcher != nil && c.Archiver != nil {
		uuidStr, _ := doc["uuid"].(string)
		u, err := c.Batcher.AddPage(ctx, uuidStr, info.PageNumber, essayPages, imgKey)
		if err != nil {
			return "", nil, false, nil // 聚合失败不 ACK，等重投
		}
		unit = u
	}

	// 试卷流：常规页按 五元组+物理页码 分流，纯作文页不发
	target := ""
	if pageKind != PageEssay {
		target = fmt.Sprintf("paper_%d_p%d", packed.UniqueID(), info.PageNumber)
	}
	return target, out, true, unit
}

// withQR 在原 JSON 上注入/覆盖 qr 字段。
// 原 payload 非 JSON 时用信封包装（_raw 存 base64），保证 quarantine 里永远是合法 JSON。
func withQR(payload []byte, info *QRInfo) []byte {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		doc = map[string]any{"_raw": base64.StdEncoding.EncodeToString(payload)}
	}
	doc["qr"] = info
	out, err := json.Marshal(doc)
	if err != nil {
		return payload
	}
	return out
}

// withClassInfo 注入 class 字段；失败原样返回。
func withClassInfo(payload []byte, info *ClassInfo) []byte {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return payload
	}
	doc["class"] = info
	out, err := json.Marshal(doc)
	if err != nil {
		return payload
	}
	return out
}
