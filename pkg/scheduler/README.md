# scheduler SDK

`iot-scheduler` 的业务侧客户端 SDK。封装 gRPC 双向流（Worker）+ unary（Submit/GetRun/CancelRun），业务方只需关心：

1. **注册 Handler**：把业务函数挂到 `jobName` 上
2. **Start**：建立长连接并自动处理派发 / 重连 / 心跳 / 优雅关闭
3. **SubmitTask**（可选）：主动入队任务

## 快速上手

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/sidchai/compkg/pkg/scheduler"
)

func main() {
    c, err := scheduler.New(scheduler.Config{
        Endpoint:       "scheduler.svc:9090",
        AppName:        "iot-cloud-platform-open",
        AppKey:         os.Getenv("SCHED_APP_KEY"),
        AppSecret:      os.Getenv("SCHED_APP_SECRET"),
        SDKVersion:     "0.1.0",
        MaxConcurrency: 50,
    })
    if err != nil {
        log.Fatalf("new scheduler client: %v", err)
    }

    // 注册必须在 Start 之前
    c.RegisterHandler("callback_retry", func(ctx context.Context, job *scheduler.Job) (string, error) {
        // 业务逻辑；返回的 string 会作为 JobResult.output 落库
        return `{"ok":true}`, nil
    })

    if err := c.Start(context.Background()); err != nil {
        log.Fatalf("start: %v", err)
    }
    defer c.Stop(context.Background())

    // 业务运行期，可随时主动提交
    runID, dedup, err := c.SubmitTask(context.Background(), scheduler.SubmitOptions{
        JobName:         "callback_retry",
        BizKey:          "record-12345",
        Payload:         []byte(`{"record_id":12345}`),
        DedupeWindowSec: 60,
    })
    log.Printf("submitted run=%s dedup=%v err=%v", runID, dedup, err)

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig
}
```

## API 速查

| 方法 | 说明 |
|------|------|
| `New(cfg) (*Client, error)` | 构造未启动的 Client，校验 Config |
| `Client.RegisterHandler(jobName, fn)` | 注册业务 handler；必须在 Start 之前 |
| `Client.Start(ctx) error` | 拨号 + 启动后台 goroutine（非阻塞） |
| `Client.Stop(ctx) error` | 优雅关闭：cancel stream + 等 inflight handler + 关连接 |
| `Client.SubmitTask(ctx, opts) (runID, dedup, err)` | 主动提交一次 API 任务 |
| `Client.GetRun(ctx, runID) (*pb.Run, error)` | 查询 run 状态 |
| `Client.CancelRun(ctx, runID, reason) error` | 取消未完成 run |

## 状态机映射

业务 handler 返回值 → scheduler 落库 `sched_run.status`：

| Handler 返回 | 上报状态 |
|--------------|----------|
| `(output, nil)` | `RUN_STATUS_SUCCESS` |
| `(_, err)` | `RUN_STATUS_FAILED`（err.Error() 进 `JobResult.error`） |
| `ctx.DeadlineExceeded` | `RUN_STATUS_TIMEOUT` |
| `ctx.Canceled`（Stop / 服务端 Cancel） | `RUN_STATUS_CANCELED` |
| panic | `RUN_STATUS_FAILED`（含 panic 值 + stack） |

## 容错与重连

| 场景 | 行为 |
|------|------|
| 启动时 scheduler 不可达 | Start 返回 dial err；业务自行决定是否重试 |
| 运行中连接断开 | 自动指数退避重连（1s → 2s → ... → 30s），稳定运行 30s 后窗口重置 |
| 服务端 Cancel 一条 run | 派生的 handler ctx 被 cancel；handler 应 select on `ctx.Done()` |
| handler panic | SDK recover，上报 FAILED，进程继续存活 |
| inflight 达到 MaxConcurrency | 新 Dispatch 直接 `Ack(accepted=false, reason="inflight full")`，服务端不算失败 |

## 配置默认值

`Config.applyDefaults()` 自动填充：

- `MaxConcurrency: 50`
- `DialTimeout: 5s`
- `HeartbeatInterval: 5s`（服务端 RegisterResponse 可覆盖）
- `ReconnectMinBackoff: 1s` / `ReconnectMaxBackoff: 30s`
- `SubmitTimeout: 5s`
- `MaxRecvMsgSizeMB: 4` / `MaxSendMsgSizeMB: 4`
- `InstanceID`: 取 `os.Hostname()`
- `WorkerID`: `{AppName}-{InstanceID}-{pid}`

## 签名算法（与服务端对齐）

```
signature = hex( HMAC-SHA256(appSecret, appKey + nonce + ts) )
```

- ts 单位秒，服务端 5min 窗口校验
- nonce 每次请求随机生成（16 字符 hex），由 SDK 自动处理
- Connect 与 SubmitTask 共用同一算法
