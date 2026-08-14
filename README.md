# scanpipe

扫描件管线的**业务接入服务**：NWBox 扫描协议替代服务端 + QR 分类工人。
消息队列 = **Redis Streams**（2026-07-28 起，替代自研 scanmq broker）。
本服务以客户端身份连接 Redis，与队列内核完全解耦——Redis 升级、重启、换机器都不影响本服务。

## 组件

```
internal/
  mqclient/   Redis Streams 薄封装：Add / Read（消费组 + XAUTOCLAIM 重投）/ Ack，
              外加 DLQ 搬运（SweepDLQ，供 janitor 周期调用）
  ingest/     NWBox 协议服务端：POST /api/upload（MD5 校验）→ raw_pages
              WS /ws?client_id= 收状态汇报（upload_finish → batch_events）
  classify/   QR 分类工人：raw_pages → gozxing 解码 → paper_<unique_id>_p<页>
              ├ qrpack.go      56 bit 打包格式编解码（规范见 docs/QR_FORMAT.md）
              ├ paper.go       hyxq 试卷模板/班级信息客户端（磁盘缓存）
              ├ archive.go     原图落对象存储（S3 兼容；class-<id> / paper-<id> bucket）
              └ essaybatch.go  作文批次聚合（Redis BITMAP 判齐，超时残缺兜底）
```

## 构建与运行

```bash
go env -w GOPROXY=https://goproxy.cn,direct   # 一次性
go mod tidy
go build ./cmd/scanpiped

# 先起 Redis（:6379），再：
./scanpiped -mq 127.0.0.1:6379 -mq-password <pwd> \
            -ingest-addr :5665 \
            -classify raw_pages -classify-workers 4 \
            -hyxq-token <hyxq token>   # 可选，不配则纯 QR 路由（无班级信息/作文判定）
```

可选：配 **S3 兼容对象存储**（RustFS / MinIO / AWS S3 等，minio-go 客户端）后，
原图落对象存储，下游消息只剩 `copy_number` + `key` 两个字段：

```bash
./scanpiped ... -rustfs-addr <host:9000> -rustfs-ak <ak> -rustfs-sk <sk>
```

> 参数名沿用 `-rustfs-*`，实际任何 S3 兼容 endpoint 均可。
> 当前实现走 HTTP（`Secure: false`）：内网 RustFS / MinIO 直接可用；
> 接 HTTPS endpoint（如公网 MinIO、AWS S3）需改 `archive.go` 的 `Secure` 选项。

所有参数均可由环境变量提供：`REDIS_ADDR` / `REDIS_PASSWORD` / `HYXQ_URL` /
`HYXQ_TOKEN` / `RUSTFS_ADDR` / `RUSTFS_AK` / `RUSTFS_SK` / `PAPER_CACHE_DIR`。

## 数据流

```
NWBox 扫描仪 ──POST /api/upload──▶ ingest ──XADD──▶ Redis[raw_pages]
                                                        │
分类工人 ◀──XREADGROUP BLOCK── Redis ◀───────────────────┘
   │ gozxing QR 解码（右上 ROI 优先，惰性候选阶梯）
   ├─ 常规页 ──XADD──▶ Redis[paper_<unique_id>_p<页>] ──▶ 批改工人
   ├─ 作文页 ──聚合成整篇──▶ Redis[essay_pages] ──▶ 作文批改工人
   └─ 识别失败 ──XADD──▶ Redis[quarantine]
```

## 注意

- 不配对象存储时消息内联 base64 原图，单条约 600KB；workers 之间通过消费组天然负载均衡
- 分类器每 worker 独立连接，BLOCK 5s，断线指数退避重连（上限 30s）；
  Redis 重启后无需重启本服务
- QR 物理页码为准，扫描序号不可信（双面扫描乱序）
- 内建 janitor：每 30s 扫描各流消费组，把投递 ≥5 次的消息搬进 `<stream>.dlq`
- 协议契约（流名、消息 schema、ACK 规则）见 [docs/PROTOCOL.md](docs/PROTOCOL.md)
