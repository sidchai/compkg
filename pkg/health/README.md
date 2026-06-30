# compkg/pkg/health 对接文档

基于 Redis ZSet 的服务实例心跳上报 + 活实例统计。

## 数据模型

```
key    = {prefix}:health:instance:{service}    -> Redis ZSet
member = instance_id（hostname-pid-uuid 等唯一标识）
score  = 最近一次心跳的 unix 秒
```

- 活实例：`score >= now - AliveWindow`（默认 30s）
- 总实例：ZSet 全部成员（含未到 StaleCleanup 的近期失联实例）

## 对接步骤（3 步）

### 1. 引入依赖

```go
import "github.com/sidchai/compkg/pkg/health"
```

go.mod 已 require `github.com/sidchai/compkg`，无需额外操作。

### 2. 服务启动时上报心跳

在 Redis 初始化完成后、服务进入主循环前调用：

```go
import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/google/uuid"
    "github.com/sidchai/compkg/pkg/health"
)

var healthReporter *health.Reporter

func StartHealth(redisClient *redis.Client) {
    if redisClient == nil {
        return
    }
    hostname, _ := os.Hostname()
    instanceID := fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString()[:8])

    healthReporter = &health.Reporter{
        Client:       redisClient,
        Service:      "your_service_name", // 必填，全局唯一
        Instance:     instanceID,          // 必填，进程级唯一
        Interval:     10 * time.Second,    // 心跳间隔，默认 10s
        StaleCleanup: 5 * time.Minute,     // 清理超期成员的阈值，默认 5min
        OnError: func(err error) {
            // 接你自己的 logger，nil 时静默
        },
    }
    _ = healthReporter.Start(context.Background())
}
```

### 3. 服务退出时注销

在 graceful shutdown 流程里调用，best-effort：

```go
func StopHealth() {
    if healthReporter != nil {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        healthReporter.Stop(ctx)
    }
}
```

`Stop` 会：关闭心跳 goroutine + `ZREM` 自己。`sync.Once` 保护，重复调用安全。

## 查询活实例（仅监控/统计端需要）

```go
reader := &health.Reader{
    Client:      redisClient,
    AliveWindow: 30 * time.Second, // 比 Reporter.Interval 大 2~3 倍
}

ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

stat, _ := reader.Stat(ctx, "your_service_name")
// stat.Running / stat.Total

// 多服务批量
stats, _ := reader.StatAll(ctx, []string{"svc_a", "svc_b"})
running, total := health.Aggregate(stats)
```

## 关键约束

| 项 | 要求 |
|---|---|
| `Service` | 跨进程一致，建议用项目仓库名 |
| `Instance` | 进程级唯一，重启必须变化（含 pid 或 uuid） |
| `Interval` vs `AliveWindow` | `AliveWindow >= Interval * 2`，防抖动误判离线 |
| `StaleCleanup` vs `AliveWindow` | `StaleCleanup >> AliveWindow`，避免清掉刚下线但还需展示的实例 |
| Redis 选型 | 单 Reporter 心跳量极小（每 10s 一个 ZADD+ZREMRANGEBYSCORE），共用业务 Redis 即可 |

## 典型故障与回退

- **Redis 不可用**：`Reporter.OnError` 触发；查询端 `Reader.Stat` 返回 error，调用方应回退为 0/0 而非阻塞接口
- **进程崩溃未注销**：依赖 `Reporter.StaleCleanup` 在下次心跳时被同服务的活实例顺手清理
- **多机时钟漂移**：`AliveWindow` 至少 30s，可容忍秒级偏差；如需严格，建议 NTP 同步
