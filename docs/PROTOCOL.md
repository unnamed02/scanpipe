# 消息队列协议契约

本文档是 scanpipe（Go）与各消费端（Python 等）之间的**唯一契约**。
双方各自实现，互不依赖内部细节。改动本文档需要双方确认。

## 1. 传输协议

**消息队列 = Redis Streams**（2026-07-28 起，替代自研 scanmq broker）。

```
redis://<redis-host>:6379
```

消息格式：stream entry 单字段 `p` = payload（JSON 字符串）。

- 入队：`XADD <stream> * p <payload>`
- 消费：`XREADGROUP GROUP <g> <c> COUNT n BLOCK ms STREAMS <stream> >`
- 确认：`XACK <stream> <group> <id>`
- 重投：`XAUTOCLAIM`（接管闲置超期消息，delivery count 由 Redis 自增）
- 死信：janitor 检查 XPENDING 的 delivery count ≥ 5 → 搬入 `<stream>.dlq`

语义即标准 Redis Streams，无 broker 侧定制；redis-py / go-redis 等任意
客户端直连即可。消息 ID 为 Redis 标准 `ms-seq` 格式（如 `1722156789123-0`）。

## 2. 流与消息 Schema

全部 JSON payload（UTF-8）：

### raw_pages — 扫描原始页（ingest → 分类工人）

```json
{
  "type": "scan_page",
  "schema": 1,
  "client_id": "24B72A466975",
  "uuid": "45f8143e-5328-4633-9e59-a2922bba6f88",
  "item_id": 1,
  "paper_number": 1,
  "page_number": 1,
  "front": true,
  "file_type": "jpg",
  "file_size": 463104,
  "md5": "fcc1c9485f93df3c10c5104dbed67a01",
  "image": "<base64 JPEG>"
}
```

- `uuid` = 一次扫描批次；`uuid + page_number` 全局唯一标识一页
- `image` 内联在消息里；MD5 针对解码后字节

### paper_<unique_id>_p<page> — 常规题已分类页（分类器 → 批改工人）

常规题是**一类流**：一个 unique_id + 页码对应一条流。消息只保留两个字段（页码已在流名里）：

```json
{"copy_number": 1, "key": "paper/00139/1/page_6.jpg"}
```

- `<unique_id>` = 43 bit 五元组的十进制（学校+班级类型+班号区+试卷编号，规则见 [QR_FORMAT.md](QR_FORMAT.md) 二.4）
- `copy_number` = 份数字段 = studentid（打包格式 bit 48-54；老格式第 2 段）
- 物理页码由流名 `paper_<unique_id>_p<n>` 承载（打包格式 bit 0-4；老格式第 4 段。扫描序号不可信）
- QR 格式见 [QR_FORMAT.md](QR_FORMAT.md)：新格式为 17 位纯数字（56 bit 打包），老格式为逗号分段，分类端两者兼容
- `key` = RustFS 对象 key；bucket 按统一规则推导：有班级信息 `class-<classId>`，否则 `paper-<paperID>`
- QR 识别失败进 `quarantine` 流（完整消息 + 内联图，供人工处理）

### essay_pages — 作文整篇（分类器 → 作文批改工人）

作文是**一条流**。分类器按扫描批次 uuid 聚合作文页，**整篇拼接完整之后再发**；
闲置超时未齐也发出（`pages_missing=true` 标记残缺）：

```json
{"uuid": "45f8143e-5328-4633-9e59-a2922bba6f88", "keys": ["paper/00139/1/page_5.jpg", "paper/00139/1/page_6.jpg"], "pages_missing": false}
```

- `keys` = 该篇全部页的 RustFS 对象 key
- 判齐依据试卷模板给出的作文物理页集合（分类器每次登记页时幂等重登），
  不依赖客户端字段；闲置 120s 未齐由兜底 sweep 残缺发出
- 作文页只参与聚合，不进 `paper_` 流；`paper_` 流承载常规题页

### grading_results — 批改结果（批改工人 → 写库工人 + 云端同步工人）

```json
{
  "type": "grading_result",
  "schema": 1,
  "ref_msg_id": "1722156789123-0",
  "uuid": "45f8143e-5328-4633-9e59-a2922bba6f88",
  "class_id": "初三2班",
  "archive_prefix": "初三2班/2026-07-27/",
  "studentid": "10342",
  "paper_id": "P2026-0512",
  "page_number": 1,
  "status": "graded",
  "grading": {
    "engine": "mmfine-reason-8b",
    "engine_version": "2026-05-21-v10",
    "part_items": [
      {
        "questionNumber": "3",
        "paperType": "其他题型",
        "questionCoord": {"x": 120, "y": 340, "width": 1500, "height": 400},
        "analyze": "对",
        "answer": "...",
        "blank": [],
        "marking": "...",
        "allinformation": {}
      }
    ],
    "duration_ms": 4200
  }
}
```

- `status`: `graded` / `need_review` / `failed`（failed 时附 `error` 字段）
- `part_items` 结构与现有 `grading_info.json` 一致，云端同步工人据此组装 TestPaper 文档

### batch_events — 批次事件（ingest → 下游整卷触发）

WS 收到扫描客户端的 `upload_finish` 时，ingest 写入一条：

```json
{
  "type": "upload_finish",
  "client_id": "24B72A466975",
  "payload": {"uuid": "...", "total_pages": 2, "success_count": 2, "fail_count": 0}
}
```

## 3. 可靠性与 ACK 规则

| 规则 | 说明 |
|---|---|
| 语义 | **at-least-once**。消费者必须幂等（写库 UPSERT、写对象存储同名覆盖） |
| ACK 时机 | **副作用完成后才 ACK**（写完 RustFS / Postgres / 完成批改并 XADD 结果后） |
| PEL 超时 | 消息闲置 ≥ 60s 未 ACK → 由其他消费者 `XAUTOCLAIM` 接管（delivery count 自增） |
| 重投上限 | delivery count ≥ 5 → janitor（scanpipe 内建，30s 周期）搬入死信流 `<stream>.dlq` |
| DLQ 格式 | `{"_dlq_reason": "max_deliveries(5) from <stream>:<id>", "payload": <原消息 JSON>}`，人工排查后重新 XADD 回流即可重处理 |
| 崩溃恢复 | Redis 持久化（AOF/RDB）保留流与 PEL，未 ACK 消息按闲置规则被接管重投 |

## 4. 背压

- Redis 侧：配置 `maxmemory` + 合适的淘汰策略，监控各流长度；生产者遇到 OOM 错误退避重试
- 消费者 prefetch=1（批改）/~8（归档、写库等轻量工人）

## 5. 顺序性

- 单流严格 FIFO；跨流不保证（`paper_*` 与 `grading_results` 之间无顺序承诺）
- 同一页的图与结果配对**不依赖 MQ 顺序**，靠 `uuid + page_number` 在 Postgres 里关联
