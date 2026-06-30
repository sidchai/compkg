// Package traffic 提供按设备号粒度的 TCP 流量统计能力。
//
// 设计目标：
//  1. 仅对白名单内设备生效（O(1) 判断），白名单外零开销
//  2. 内存原子累加 + 秒级 flush 到 Redis（避免单连接每秒上千次 Read 打爆 Redis）
//  3. Redis Hash 跨服务原子累加（多实例自动汇总到同一小时桶）
//  4. 每小时整点 cron 由"承担落库职责的实例"调用 DumpPending，扫描所有非当前小时桶 UPSERT 到 MySQL
//
// 使用流程：
//
//	traffic.Init(traffic.Config{ ... })   // 启动时初始化一次
//	conn = traffic.Wrap(conn)              // 包装 net.Conn
//	conn.SetDeviceNo(deviceNo)             // 握手解析到设备号后调用，激活统计
//	// 后续 conn.Read / conn.Write 自动统计上/下行
//
//	// 仅 dump 节点（通常一个服务）的 cron：
//	traffic.DumpPending(ctx)
package traffic

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// Config 流量统计模块配置。
type Config struct {
	// ServiceName 服务名（如 iot_server / audio_self_host），用于区分多服务的同设备流量。必填。
	ServiceName string

	// Redis 客户端，多服务必须连同一个 Redis 实例才能跨服务汇总。必填。
	Redis *redis.Client

	// DB GORM 客户端。仅"承担小时落库职责"的实例需要传入；其他实例传 nil 表示只埋点不落库。
	// 整套系统通常只有一个实例传 DB，避免重复落库与并发争抢。
	DB *gorm.DB

	// DeviceWhitelist 监控设备号白名单。白名单外的设备 read/write 不统计（零开销）。必填。
	DeviceWhitelist []string

	// KeyPrefix Redis Hash Key 前缀，默认 "traffic:hourly:"
	// 完整 Key 形如 traffic:hourly:2026052018（YYYYMMDDHH）
	KeyPrefix string

	// FlushInterval 内存 → Redis flush 间隔，默认 1s
	FlushInterval time.Duration

	// HourKeyTTL Redis Hash 兜底 TTL，默认 25h（>1 小时，给 cron 充足兜底时间）
	HourKeyTTL time.Duration

	// TableName MySQL 表名，默认 "device_traffic_hourly"
	TableName string
}

var (
	globalTracker *tracker
	initOnce      sync.Once
	initErr       error
)

// Init 初始化全局 tracker。多次调用只生效第一次。
//
// 若 cfg 校验失败返回 error，调用方应 panic 或退出。
func Init(cfg Config) error {
	initOnce.Do(func() {
		initErr = doInit(cfg)
	})
	return initErr
}

func doInit(cfg Config) error {
	if cfg.ServiceName == "" {
		return errors.New("traffic: ServiceName required")
	}
	if cfg.Redis == nil {
		return errors.New("traffic: Redis required")
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "traffic:hourly:"
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = time.Second
	}
	if cfg.HourKeyTTL <= 0 {
		cfg.HourKeyTTL = 25 * time.Hour
	}
	if cfg.TableName == "" {
		cfg.TableName = "device_traffic_hourly"
	}

	whitelist := make(map[string]struct{}, len(cfg.DeviceWhitelist))
	for _, no := range cfg.DeviceWhitelist {
		if no != "" {
			whitelist[no] = struct{}{}
		}
	}

	globalTracker = newTracker(cfg, whitelist)
	globalTracker.start()
	return nil
}

// IsTracked 判断设备号是否在监控白名单（O(1)）。
// 未 Init 时恒返回 false。
func IsTracked(deviceNo string) bool {
	if globalTracker == nil {
		return false
	}
	return globalTracker.isTracked(deviceNo)
}

// AddUp 累加上行字节数（设备 → 服务）。n<=0 或设备不在白名单时静默忽略。
// 推荐使用 Wrap + SetDeviceNo 自动统计；本函数留给特殊场景手工调用。
func AddUp(deviceNo string, n int) {
	if globalTracker == nil || n <= 0 {
		return
	}
	globalTracker.addUp(deviceNo, int64(n))
}

// AddDown 累加下行字节数（服务 → 设备）。n<=0 或设备不在白名单时静默忽略。
func AddDown(deviceNo string, n int) {
	if globalTracker == nil || n <= 0 {
		return
	}
	globalTracker.addDown(deviceNo, int64(n))
}

// DumpPending 扫描所有非当前小时的 Redis Hash 并 UPSERT 到 MySQL。
// 通常由 cron 在每小时整点（建议 +10s 安全延迟）调用。
//
// 设计要点：
//   - 仅处理"非当前小时"的 Hash，当前小时仍在累加中，跳过
//   - UPSERT 用唯一键 (stat_time, device_no, service)，重跑覆盖等价
//   - 落库失败保留 Redis Key，下次 cron 自然重试
//   - 落库成功才删 Key
//
// 未 Init 或 DB 未配置时返回 error。
func DumpPending(ctx context.Context) error {
	if globalTracker == nil {
		return errors.New("traffic: not initialized")
	}
	if globalTracker.cfg.DB == nil {
		return errors.New("traffic: DB not configured, this instance is not responsible for dumping")
	}
	return globalTracker.dumpPending(ctx)
}

// Stop 优雅退出：flush 残留内存增量到 Redis，停止后台 ticker。
// 通常在进程退出时调用一次。
func Stop() {
	if globalTracker == nil {
		return
	}
	globalTracker.stop()
}

// Wrap 把 net.Conn 包装成具备流量统计能力的 TrackedConn。
//
// 工作模式：
//  1. 返回的 *TrackedConn 实现 net.Conn，可直接替换原 conn 使用
//  2. 初始 deviceNo 为空，所有 Read/Write 都不统计（零开销）
//  3. 业务层握手解析到设备号后调用 SetDeviceNo 激活
//  4. 仅白名单内设备会实际累加，其他设备虽 SetDeviceNo 也只是空设值
func Wrap(conn net.Conn) *TrackedConn {
	return newTrackedConn(conn)
}
