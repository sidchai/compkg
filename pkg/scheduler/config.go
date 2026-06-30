package scheduler

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Config 是创建 Client 的参数。所有字段都有合理默认值，仅 Endpoint / AppName / AppKey / AppSecret 必填。
type Config struct {
	// === 必填 ===

	// Endpoint scheduler 服务的 gRPC 地址，如 "scheduler.svc:9090"
	Endpoint string

	// AppName 与 sched_app.app_name 一致；服务端据此鉴权与限流
	AppName string

	// AppKey / AppSecret 业务方在 scheduler UI 注册应用时领取
	AppKey    string
	AppSecret string

	// === 可选 ===

	// WorkerID 客户端实例的全局唯一标识；为空时自动生成 "{AppName}-{hostname}-{pid}"
	WorkerID string

	// InstanceID k8s pod name / hostname；为空时取 os.Hostname()
	InstanceID string

	// IP 实例 IP，用于排障显示；为空时不上报
	IP string

	// SDKVersion 业务方填自己的版本号，用于服务端兼容性检查
	SDKVersion string

	// MaxConcurrency worker 同时执行的最大任务数；超过时新 Dispatch 会被 Ack(accepted=false)
	// 默认 50
	MaxConcurrency int

	// DialTimeout grpc.Dial 超时；默认 5s
	DialTimeout time.Duration

	// HeartbeatInterval 心跳发送间隔；服务端 RegisterResponse 会覆盖此值
	// 默认 5s
	HeartbeatInterval time.Duration

	// ReconnectMinBackoff / ReconnectMaxBackoff 断线重连的指数退避区间
	// 默认 1s ~ 30s
	ReconnectMinBackoff time.Duration
	ReconnectMaxBackoff time.Duration

	// SubmitTimeout SubmitTask unary 调用超时；默认 5s
	SubmitTimeout time.Duration

	// MaxRecvMsgSizeMB / MaxSendMsgSizeMB gRPC 消息大小上限；默认 4MB
	MaxRecvMsgSizeMB int
	MaxSendMsgSizeMB int

	// === 本地兜底 buffer（仅 EnqueueTask 使用）===

	// LocalBufferEnabled 启用内存兜底队列。开启后可调用 EnqueueTask；
	// scheduler 不可达时任务缓存在内存，恢复后后台 goroutine 自动重发。
	// 默认 false。
	LocalBufferEnabled bool

	// LocalBufferCapacity 兜底队列最大容量；满时 EnqueueTask 返回 ErrLocalBufferFull。
	// 默认 1024。
	LocalBufferCapacity int

	// LocalBufferRetryInterval 后台重发轮询间隔；默认 5s。
	LocalBufferRetryInterval time.Duration

	// LocalBufferRetryBatch 单次轮询最多重发条数；默认 32。
	LocalBufferRetryBatch int

	// LocalBufferDiskSpillEnabled 启用磁盘二级队列。
	// 内存队列满时任务会落到 LocalBufferDiskSpillDir，进程重启后按 FIFO 重放。
	// 仅对 EnqueueTask 生效；SubmitTask 的同步语义不变。默认 false。
	LocalBufferDiskSpillEnabled bool

	// LocalBufferDiskSpillDir 磁盘 spill 目录；为空时使用 os.TempDir()/scheduler-spool。
	LocalBufferDiskSpillDir string

	// LocalBufferDiskSpillMaxBytes 磁盘 spill 总大小上限；默认 100MB。
	// 超限时优先淘汰最旧任务，避免 SDK 兜底队列无限占满磁盘。
	LocalBufferDiskSpillMaxBytes int64
}

// applyDefaults 填充未设置的字段；调用方再 Validate() 即可。
func (c *Config) applyDefaults() {
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = 50
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 5 * time.Second
	}
	if c.ReconnectMinBackoff <= 0 {
		c.ReconnectMinBackoff = 1 * time.Second
	}
	if c.ReconnectMaxBackoff <= 0 {
		c.ReconnectMaxBackoff = 30 * time.Second
	}
	if c.SubmitTimeout <= 0 {
		c.SubmitTimeout = 5 * time.Second
	}
	if c.MaxRecvMsgSizeMB <= 0 {
		c.MaxRecvMsgSizeMB = 4
	}
	if c.MaxSendMsgSizeMB <= 0 {
		c.MaxSendMsgSizeMB = 4
	}
	if c.LocalBufferCapacity <= 0 {
		c.LocalBufferCapacity = 1024
	}
	if c.LocalBufferRetryInterval <= 0 {
		c.LocalBufferRetryInterval = 5 * time.Second
	}
	if c.LocalBufferRetryBatch <= 0 {
		c.LocalBufferRetryBatch = 32
	}
	if c.LocalBufferDiskSpillMaxBytes <= 0 {
		c.LocalBufferDiskSpillMaxBytes = defaultDiskSpillMaxBytes
	}
	if c.InstanceID == "" {
		if h, err := os.Hostname(); err == nil {
			c.InstanceID = h
		} else {
			c.InstanceID = "unknown"
		}
	}
	if c.WorkerID == "" {
		c.WorkerID = fmt.Sprintf("%s-%s-%d", c.AppName, c.InstanceID, os.Getpid())
	}
}

// Validate 校验必填字段；applyDefaults 后调用。
func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return errors.New("scheduler: Config.Endpoint required")
	}
	if c.AppName == "" {
		return errors.New("scheduler: Config.AppName required")
	}
	if c.AppKey == "" {
		return errors.New("scheduler: Config.AppKey required")
	}
	if c.AppSecret == "" {
		return errors.New("scheduler: Config.AppSecret required")
	}
	if c.ReconnectMinBackoff > c.ReconnectMaxBackoff {
		return errors.New("scheduler: ReconnectMinBackoff must <= ReconnectMaxBackoff")
	}
	return nil
}
