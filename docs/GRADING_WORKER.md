# 批改 Worker（grading worker）设计文档

> 面向维护者。本文件是该模块的**唯一权威契约**：消息格式、数据流、
> 错误处理、幂等规则都以本文为准。改动前请先改文档并通知上下游。

## 1. 职责

消费按"卷 + 页"分流的轻消息，完成单页批改：

```
Redis Stream: paper_<unique_id>_p<page>
  → 拉取原图（RustFS）
  → 拉取试卷模板（hyxq，含题目坐标/答案/判题标准）
  → 逐题切图 → 调多模态模型批改（mmfine @ 推理服务器）
  → 画批改标记图（对号/叉号/标准答案标签）
  → 产出：批改结果消息 + 标记图回写 RustFS
```

**不做什么**：不做 QR 识别（上游已完成）、不做班级路由（上游已完成）、
不直接写云端数据库（由下游 cloud-sync worker 订阅结果流完成）。

## 2. 上游契约（输入）

### 流命名

```
paper_<unique_id>_p<page>     例：paper_95122686091_p6
```

- unique_id：43 bit 五元组十进制（学校+班级类型+班号区+试卷编号，规则见 [QR_FORMAT.md](QR_FORMAT.md) 二.4）
- page：QR 物理页码（扫描序号不可信，双面扫描乱序）

### 消息格式（60B 轻消息）

```json
{"copy_number": 1, "key": "paper/00139/1/page_6.jpg"}
```

| 字段 | 含义 |
|---|---|
| `copy_number` | QR 第二位 = studentid（份数即学号，印刷时绑定学生） |
| `key` | RustFS 对象 key，格式 `paper/<paper_id>/<studentid>/page_<n>.jpg` |

### bucket 推导规则（**必须与此一致**）

消息里**没有 bucket 字段**，worker 按统一规则推导：

```
有班级信息（hyxq 接口返回 classIds/schoolClassId）→ class-<第一个ID>
否则                                              → paper-<paperID>
```

实现参考：`scanpipe/internal/classify/archive.go` 的 `BucketFor`。
任何一侧改规则，两侧必须同时改。

### 消费组与 ACK

- 消费组名：`graders`；consumer 名：`grader-<host>-<pid>-<n>`
- 读取：`XREADGROUP GROUP graders <consumer> COUNT 1 BLOCK 5000 STREAMS <stream> >`
- **ACK 时机：所有副作用完成后**（结果消息发出 + 标记图写回 RustFS 成功）
- 重投：worker 崩溃/超时未 ACK，由 `XAUTOCLAIM` 接管（见 §6）

## 3. 输出契约

### 3.1 结果流 `grading_results`

```json
{
  "paper_id": "00139",
  "copy_number": 1,
  "page_number": 6,
  "version": "99",
  "class_type": "gradeClass",
  "status": "graded",
  "source_key": "paper/00139/1/page_6.jpg",
  "marked_key": "paper/00139/1/page_6_marked.jpg",
  "grading": {
    "engine": "mmfine-reason-8b",
    "engine_version": "2026-05-21-v10",
    "duration_ms": 4200,
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
    ]
  }
}
```

- `status`：`graded` / `need_review`（有 need_review 空）/ `failed`（附 `error` 字段）
- `part_items` 结构**与 mycorrect1.4 的 grading_info.json 完全一致**——
  下游云端同步 worker 直接复用现有 TestPaper 文档组装逻辑
- `marked_key`：批改标记图（整页，含对号/叉号/标准答案标签）

### 3.2 产物回写（RustFS 三件套）

```
bucket：与原图同 bucket
原图：     paper/<pid>/<sid>/page_<n>.jpg            （分类器写入）
标记图：   paper/<pid>/<sid>/page_<n>_marked.jpg     （worker 回传）
结果 JSON：paper/<pid>/<sid>/page_<n>_result.json    （worker 回传）
```

结果消息中携带 source_key / marked_key / result_key 三个指针。

## 4. 外部依赖

| 依赖 | 地址 | 用途 | 失败处理 |
|---|---|---|---|
| Redis | 6379 | 消息收发 | 断线重连（指数退避，上限 30s） |
| RustFS | 9000 | 拉图/写标记图 | 单次失败不 ACK，等重投 |
| mmfine（主模型） | 192.168.0.102:8000 | 逐题看图批改 | 重试 3 次 → failed 消息 |
| PaddleOCR-VL | 192.168.0.102:8001 | 手写识别兜底 | 降级为仅模型判定 |
| hyxq | https://hyxq.com.cn | 试卷模板 + 班级信息 | 本地缓存，缓存外失败 → failed |

### 试卷模板缓存

模板（题目坐标、标准答案、判题标准 rubric）按 `paper_id + version + class_type`
缓存到本地磁盘（复用 mycorrect1.4 `server_manager` 的缓存目录规则），
模板不变不重复拉取。

## 5. 幂等规则（at-least-once 世界里的生存法则）

MQ 语义是 at-least-once，**同一条消息可能被批改多次**，必须幂等：

| 副作用 | 幂等机制 |
|---|---|
| 标记图写 RustFS | 同 key 覆盖写，天然幂等 |
| 结果消息重发 | 下游按 `(paper_id, copy_number, page_number)` UPSERT |
| 模型调用 | 无状态，重复调用只费钱不脏数据 |

**禁止**：在结果消息里携带"第几次处理"之类的状态；每次处理都必须产出完整结果。

## 6. 错误处理矩阵

| 场景 | 行为 | 最终去向 |
|---|---|---|
| 拉图失败（key 不存在/网络） | 不 ACK | PEL 超时重投，5 次后 DLQ |
| 模板拉取失败 | 发 `status=failed` 消息 + ACK | 结果流（failed） |
| 模型调用失败（重试 3 次） | 发 `status=failed` + error + ACK | 结果流（failed） |
| 部分题需人工复核 | `status=need_review`，该空 need_review=true | 复核界面 |
| 消息格式非法 | 直接 ACK + 记日志（毒消息不重投） | 日志 |

DLQ（`paper_*_p*.dlq`）由 janitor 搬运，人工排查后重新 XADD 回流即可重处理。

## 7. 并发模型

- 每 worker 进程 N 个协程/线程（默认 8），各自独立 Redis 连接
- 同组内 broker 自动分发；**同一页不会被同组两个 worker 同时拿到**
  （Redis 消费组语义：一条消息同一时刻只投递给组内一个 consumer）
- 吞吐瓶颈是 mmfine 推理（3~8s/页），worker 数量按推理服务器并发容量配置

## 8. 配置项

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `REDIS_ADDR` | 127.0.0.1:6379 | Redis 地址 |
| `REDIS_PASSWORD` | 空 | |
| `RUSTFS_ADDR` / `RUSTFS_AK` / `RUSTFS_SK` | — | 对象存储 |
| `MODEL_API_URL` | http://192.168.0.102:8000/v1/chat/completions | 多 URL 逗号分隔可轮询 |
| `MODEL_NAME` | mmfine-reason-8b | |
| `PADDLEOCR_VL_BASE_URL` | http://192.168.0.102:8001 | |
| `HYXQ_URL` / `HYXQ_TOKEN` | — | 模板与班级信息 |
| `GRADER_WORKERS` | 8 | 并发工人数 |

## 9. 本地开发

```bash
# 依赖：本地 Redis + RustFS（docker compose 一键起，见 scanpipe/README）
docker run -d --name scanredis -p 6379:6379 redis:7-alpine
docker run -d --name rustfs -p 9000:9000 -p 9001:9001 rustfs/rustfs:latest

# 造一页测试数据：用 scanpipe 的模拟扫描
python scanpipe/tools/simulate_scan.py http://127.0.0.1:5665 test-001 ../page_1.jpg ../page_2.jpg

# 起 worker（实现语言：Python，复用 mycorrect1.4 的 OCRProcessor/prompts）
python grader/worker.py --streams paper_95122686091_p5,paper_95122686091_p6

# 验证
docker exec scanredis redis-cli xreadgroup GROUP check c1 COUNT 5 STREAMS grading_results
```

## 10. 维护者须知

1. **输入消息就两个字段**，任何"还想知道什么"都从 key 解析或调接口，不要给消息加字段
2. 模型 prompt 复用 `mycorrect1.4/core/prompts.py`，改 prompt 要先理解里面的
   防"抄标准答案"规则（那是多次线上事故换来的）
3. 画标记的坐标全部来自试卷模板 + 单应性校正，**不要让模型输出坐标**
4. 新题型支持：先在 prompts.py 的题型清单注册，再确认 strict/semantic/open 判定分支
5. 性能优化只允许两个方向：推理批处理、预取下一页（见设计讨论，图不过内存）
