# scanpipe

扫描件管线的**业务接入服务**：NWBox 扫描协议替代服务端 + QR 分类工人。
以客户端身份连接 scanmq（纯 broker），与队列内核完全解耦——broker 升级、重启、换机器都不影响本服务。

## 与 scanmq 的分工

| | scanmq | scanpipe（本项目） |
|---|---|---|
| 角色 | 纯消息队列 broker | 业务组件：扫描接入 + QR 分类 |
| 协议 | RESP 服务端（MQ* 命令） | RESP 客户端（internal/mqclient）+ HTTP/WS 服务端 |
| 状态 | 段日志 + Pebble，有状态 | 无状态（除 classinfo 内存缓存），可随意重启 |

## 组件

```
internal/
  mqclient/   scanmq 极简 RESP 客户端（单连接+锁；阻塞读每 worker 一条连接）
  ingest/     NWBox 协议服务端：POST /api/upload（MD5校验）→ raw_pages
              WS /ws?client_id= 收状态汇报（upload_finish 钩子预留）
  classify/   QR 分类工人：raw_pages → gozxing 解码 → paper_<id>_p<页>
              可选调 hyxq 班级信息接口注入 class 字段
```

## 构建与运行

```bash
go env -w GOPROXY=https://goproxy.cn,direct   # 一次性
go mod tidy
go build ./cmd/scanpiped

# 先起 scanmq（:6380），再：
./scanpiped -mq 192.168.0.110:6380 -mq-password <token> \
            -ingest-addr :5665 \
            -classify raw_pages -classify-workers 8 \
            -hyxq-token <hyxq token>   # 可选，不配则纯 QR 路由
```

## 数据流

```
NWBox 扫描仪 ──POST /api/upload──▶ ingest ──MQADD──▶ scanmq[raw_pages]
                                                          │
分类工人 ◀──MQREAD BLOCK── scanmq ◀──────────────────────┘
   │ gozxing QR 解码（右上ROI优先，惰性候选阶梯）
   ├─ 成功 ──MQADD──▶ scanmq[paper_<id>_p<页>] ──▶ 批改/归档工人
   └─ 失败 ──MQADD──▶ scanmq[quarantine]
```

## 注意

- 消息里的图片是 base64 内联，单条约 600KB；workers 之间通过消费组天然负载均衡
- 分类器每条连接 BLOCK 5s，断线会每秒重试；scanmq 重启后无需重启本服务（TCP 会自动断，
  当前版本遇到连接错误会继续空转——生产部署建议用 supervisor 拉起，进程退出即重启）
- QR 物理页码为准，扫描序号不可信（双面扫描乱序）
