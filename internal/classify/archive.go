// archive.go 分类后的落盘组件：原图写入 RustFS（S3 兼容）。
// 只存图，不存元数据 JSON——与原项目设计一致：
// 图片走对象存储，元数据走队列消息/数据库。
//
// 按班级建 bucket（S3 命名规范不允许中文/大写，已实测 RustFS 服务端拒绝）：
//
//	bucket = class-<classId>     班级信息可用（classIds/schoolClassId 第一个）
//	bucket = paper-<paperID>     无班级信息时的降级
//	bucket = quarantine          最终兜底
//
// bucket 内 key 与 hyxq 云存储对齐：paper/<paper_id>/<studentid>/page_<n>.jpg
package classify

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Archiver 封装 RustFS 写入，按目标 bucket 惰性创建（进程内缓存已建集合）。
type Archiver struct {
	cli     *minio.Client
	ensured sync.Map // bucket -> struct{}
}

func NewArchiver(endpoint, accessKey, secretKey string) (*Archiver, error) {
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}
	return &Archiver{cli: cli}, nil
}

// BucketFor 计算目标 bucket 名（DNS 安全：小写字母/数字/连字符）。
func BucketFor(info *ClassInfo, paperID string) string {
	if info != nil {
		if len(info.SchoolClassID) > 0 {
			return fmt.Sprintf("class-%d", info.SchoolClassID[0])
		}
		if len(info.ClassIDs) > 0 {
			return fmt.Sprintf("class-%d", info.ClassIDs[0])
		}
	}
	if id := sanitizeBucketPart(paperID); id != "" {
		return "paper-" + id
	}
	return "quarantine"
}

func sanitizeBucketPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ensureBucket 首次使用时创建 bucket。
func (a *Archiver) ensureBucket(ctx context.Context, bucket string) error {
	if _, ok := a.ensured.Load(bucket); ok {
		return nil
	}
	exists, err := a.cli.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := a.cli.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return err
		}
	}
	a.ensured.Store(bucket, struct{}{})
	return nil
}

// PutImage 落盘一页原图，返回对象 key。
func (a *Archiver) PutImage(ctx context.Context, bucket, key string, jpegData []byte) error {
	if err := a.ensureBucket(ctx, bucket); err != nil {
		return err
	}
	_, err := a.cli.PutObject(ctx, bucket, key, bytes.NewReader(jpegData),
		int64(len(jpegData)), minio.PutObjectOptions{ContentType: "image/jpeg"})
	return err
}

// KeyBase 组装对象 key 前缀（不含扩展名），与 hyxq 云存储路径语义对齐：
//
//	paper/<paper_id>/<studentid>/page_<n>
//
// studentid 即 QR 第二位 copy_number；同目录下放同名 .json 元数据。
func KeyBase(paperID, studentID string, pageNumber int) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' {
				return '_'
			}
			return r
		}, strings.TrimSpace(s))
	}
	return fmt.Sprintf("paper/%s/%s/page_%d", clean(paperID), clean(studentID), pageNumber)
}
