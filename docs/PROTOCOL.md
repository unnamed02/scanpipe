# scanmq 协议契约

本文档是 Go broker（scanmq）与 Python 消费端之间的**唯一契约**。
双方各自实现，互不依赖内部细节。改动本文档需要双方确认。

## 1. 传输协议

**消息队列 = Redis Streams**（2026-07-28 起，替代自研 scanmq broker）。

```
redis://<redis-host>:6379
```

消息格式：stream entry 单字段 `p` = payload（JSON 字符串）。
- 入队：`XADD <stream> * p <payload>`
- 消费：`XREADGROUP GROUP <g> <c> COUNT n BLOCK ms STREAMS <stream> >`
- 重投：`XAUTOCLAIM`（接管闲置超期消息，delivery count 由 Redis 自增）
- 死信：janitor 检查 XPENDING 的 delivery count ≥5 → 搬入 `<stream>.dlq`

与真实 Redis 的**差异点**（redis-py 不会校验，但语义以本文档为准）：

- 消息 ID 是 **uint64 offset 的十进制字符串**（如 `"10485"`），不是 Redis 的 `ms-seq` 格式
- `MQADD` 简化为单 payload：`MQADD <stream> <payload>`，返回 ID 字符串
- payload 即完整消息体（JSON 字符串或二进制），不做 field-value 拆分

## 2. 命令子集

### MQADD — 入队

```
MQADD <stream> <payload>
→ 返回: "10485"  (消息 ID，单调递增)
```

### MQREAD — 消费组读取

```
MQREAD <stream> <group> <consumer> [COUNT n] [BLOCK ms]
→ 返回: [[id, payload], ...]  （至多 n 条新消息）
```

语义：

- 每条流按 group 记录 `last_delivered` 偏移；读 `>` 即返回其后的新消息
- 投递即入 PEL（Pending Entries List），consumer 记名
- `BLOCK ms`：无新消息时连接挂起等待，任一流有新消息或超时即返回（超时返回空数组）。唤醒精度为毫秒级；推荐消费者统一使用 `BLOCK 5000` 代替轮询

### MQACK — 确认

```
MQACK <stream> <group> <id>
→ 返回: 1 (从 PEL 移除) / 0 (不在 PEL)
```

### MQSTATS — 队列指标

```
MQSTATS [stream]
→ 返回: [[stream, depth, [[group, next_offset, lag, pel_size], ...]], ...]
```

### AUTH — 认证（broker 以 -auth 启动时）

```
AUTH <token>  → "OK"（连接级，每个连接认证一次；redis-py 用 password= 参数自动完成）
```

### MQPENDING — 查看未确认

```
MQPENDING <stream> <group>
→ 返回: [[id, consumer, deliver_unix_ms, attempts], ...]
```

### PING

```
PING → "PONG"
```

## 3. 流与消息 Schema

三条流，全部 JSON payload（UTF-8）：

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
- `image` 内联在消息里（不落盘设计的核心）；MD5 针对解码后字节

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
超时未齐也发出（`pages_missing=true` 标记残缺）：

```json
{"uuid": "45f8143e-5328-4633-9e59-a2922bba6f88", "keys": ["paper/00139/1/page_5.jpg", "paper/00139/1/page_6.jpg"], "pages_missing": false}
```

- `keys` = 该篇全部页的 RustFS 对象 key，按扫描序排列
- 应到页数以 `batch_events` 的 `upload_finish`（total_pages）为准；finish 只记页数，攒满才发
- 作文页只参与聚合，不进 `paper_` 流；`paper_` 流承载常规题页

### grading_results — 批改结果（批改工人 → 写库工人 + 云端同步工人）

```json
{
  "type": "grading_result",
  "schema": 1,
  "ref_msg_id": "10512",
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

## 4. 可靠性与 ACK 规则

| 规则 | 说明 |
|---|---|
| 语义 | **at-least-once**。消费者必须幂等（写库 UPSERT、写对象存储同名覆盖） |
| ACK 时机 | **副作用完成后才 ACK**（写完 RustFS / Postgres / 完成批改并 XADD 结果后） |
| PEL 超时 | 消息超时未 ACK（`-pel-timeout`，默认 300s）→ 下次 MQREAD 自动重投（attempts+1，换新消费者名下） |
| 重投上限 | attempts ≥ `-max-attempts`（默认 5）且已超时 → sweeper 移入死信流 `<stream>.dlq`；JSON 消息原样注入 `_dlq_reason`，非 JSON 用信封包装（`payload_base64`） |
| 重投时机 | 补在新消息之后：MQREAD 先发新消息，不足 COUNT 时才补发 PEL 超时消息 |
| 段 GC | sweeper 周期（`-sweep-interval`）整段删除：水位 = min(各组待投递偏移, PEL 消息 ID)；**无消费组的流水位锁死，不删** |
| 崩溃恢复 | broker 重启后消息（段日志）与 PEL（Pebble）全量保留，未 ACK 消息按超时规则重投 |

## 5. 背压

- broker 内存水位：堆中未落盘消息 > 2GB → MQADD 返回 `ERR backpressure`，生产者退避重试
- 消费者 prefetch=1（批改）/~8（归档、写库等轻量工人）

## 6. 顺序性

- 单流严格 FIFO；跨流不保证（classified_pages 与 grading_results 之间无顺序承诺）
- 同一页的图与结果配对**不依赖 MQ 顺序**，靠 `uuid + page_number` 在 Postgres 里关联
