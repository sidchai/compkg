package redishealth

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	redis "github.com/go-redis/redis/v8"
	"github.com/google/uuid"

	"github.com/sidchai/compkg/pkg/buildinfo"
	"github.com/sidchai/compkg/pkg/health"
)

// Options 心跳上报可选参数；零值即可用，业务一般只需传 client + service。
type Options struct {
	Interval     time.Duration     // 心跳间隔，默认 10s
	StaleCleanup time.Duration     // 过期实例清理阈值，默认 5min
	Prefix       string            // health key 前缀，默认 compkg；须与检测工具一致
	ExtraMeta    map[string]string // 业务追加的自定义元信息（会与默认 meta 合并）
	OnError      func(err error)   // 心跳错误回调，nil 时静默
}

// BuildMeta 组装实例元信息：版本指纹 + 运行时身份。
//
// 发版检测工具据此判定"运行中的进程 == 本次发布的二进制"：
//   - commit/buildID：证明跑的是哪个源码版本（验"代码是新的"）
//   - startTime：证明进程在发版后重启过（验"进程换了"）
//   - pid/binPath：辅助外部 SSH 探测对账
func BuildMeta(extra map[string]string) (instanceID string, meta map[string]string) {
	hostname, _ := os.Hostname()
	instanceID = fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString()[:8])
	binPath, _ := os.Executable()
	meta = map[string]string{
		"buildID":   buildinfo.BuildID(),
		"commit":    buildinfo.GitCommit,
		"buildTime": buildinfo.BuildTime,
		"version":   buildinfo.Version,
		"startTime": strconv.FormatInt(time.Now().Unix(), 10),
		"pid":       strconv.Itoa(os.Getpid()),
		"host":      hostname,
		"binPath":   binPath,
	}
	for k, v := range extra {
		meta[k] = v
	}
	return instanceID, meta
}

// Start 一行式启动带版本指纹的心跳上报。
//
// 返回的 Reporter 用于优雅停止（进程退出时调 Stop 注销实例）；启动失败返回 error。
// 典型用法：
//
//	reporter, err := redishealth.Start(ctx, redisClient, "iot_cloud_platform_server", redishealth.Options{})
//	defer reporter.Stop(context.Background())
func Start(ctx context.Context, client redis.UniversalClient, service string, opts Options) (*health.Reporter, error) {
	if client == nil {
		return nil, fmt.Errorf("redishealth: nil redis client")
	}
	if service == "" {
		return nil, fmt.Errorf("redishealth: empty service name")
	}
	if opts.Interval <= 0 {
		opts.Interval = 10 * time.Second
	}
	if opts.StaleCleanup <= 0 {
		opts.StaleCleanup = 5 * time.Minute
	}

	instanceID, meta := BuildMeta(opts.ExtraMeta)
	reporter := &health.Reporter{
		Store:        NewStore(client),
		Service:      service,
		Instance:     instanceID,
		Prefix:       opts.Prefix,
		Interval:     opts.Interval,
		StaleCleanup: opts.StaleCleanup,
		Meta:         meta,
		OnError:      opts.OnError,
	}
	if err := reporter.Start(ctx); err != nil {
		return nil, fmt.Errorf("redishealth: start reporter: %w", err)
	}
	return reporter, nil
}

// NewReader 构造一个带 redis 实现的 health.Reader，供服务端查询活实例/实例明细。
func NewReader(client redis.UniversalClient, prefix string, aliveWindow time.Duration) *health.Reader {
	return &health.Reader{
		Store:       NewStore(client),
		Prefix:      prefix,
		AliveWindow: aliveWindow,
	}
}
