// Package logger 提供统一日志库（基于 zap + lumberjack）。
//
// 特性（与 RFC-06 §统一日志库 对齐）：
//   - JSON 结构化日志 + 强字段契约（ts/level/msg/service/env/version/host/trace_id/span_id/...）
//   - 业务字段自动透传（device_no / account_id / msg_id 等）
//   - trace_id / span_id 通过可插拔 TraceExtractor 自动注入
//   - 内置脱敏规则 + 字段级覆盖
//   - 级别热更新（与 RFC-06 配置中心协同）
//   - 高频日志采样
//   - hlog.FullLogger 适配（业务侧 hlog.* 调用零改动）
//
// 使用顺序：Bootstrap -> hlog.SetLogger(HlogAdapter()) -> 业务调用。
package logger

import (
	"time"
)

// 注：Level / LevelTrace / LevelDebug / ... / LevelFatal 在 logger.go 中已定义（hertz 风格 7 级），
// 本包复用，不再重复声明。

// BootstrapOptions 启动选项。
type BootstrapOptions struct {
	// ServiceName 服务名（写入每条日志 service 字段）。必填。
	ServiceName string

	// Env 环境（dev/test/prod/private）。
	Env string

	// Version 服务版本（来自构建注入或配置）。
	Version string

	// Host 主机名；为空时自动取 os.Hostname()。
	Host string

	// Level 初始日志级别；可被 SetLevel 热修改。
	Level Level

	// Format "json"（默认） 或 "console"（彩色，便于 dev 调试）。
	Format string

	// File 文件落盘配置；File.Path 为空时不开文件 sink。
	File FileSinkOptions

	// EnableStdout 是否额外打 stdout（容器场景常用）。
	EnableStdout bool

	// SanitizeRules 脱敏规则；为空时使用默认规则（DefaultSanitizeRules）。
	// 设为 DisableSanitize 可关闭脱敏。
	SanitizeRules []SanitizeRule

	// SampleEveryN Debug/Info 高频日志采样：每 N 条留 1 条。0 = 不采样。
	// 注意：Warn 及以上级别永不采样。
	SampleEveryN int

	// AddCaller 是否在日志中加入调用位置（caller 字段）。默认 true。
	AddCaller bool

	// CallerSkip 调用栈跳过的层数；仅在通过 hlog 包装层调用时需要调整。
	CallerSkip int
}

// FileSinkOptions lumberjack 文件落盘配置。
type FileSinkOptions struct {
	// Path 日志文件路径，例如 /var/log/iot/s3_iot_server.log。
	// 为空时不启用文件 sink。
	Path string

	// MaxSizeMB 单文件最大尺寸（MB）。默认 200。
	MaxSizeMB int

	// MaxBackups 保留的旧文件数。默认 30。
	MaxBackups int

	// MaxAgeDays 文件最大保留天数。默认 30。
	MaxAgeDays int

	// Compress 是否 gzip 压缩旧文件。默认 false。
	Compress bool

	// LocalTime 文件名时间使用本地时区。默认 true。
	LocalTime bool
}

// 默认值常量。
const (
	defaultMaxSizeMB  = 200
	defaultMaxBackups = 30
	defaultMaxAgeDays = 30
)

// applyDefaults 给 BootstrapOptions 填充默认值。
func (o *BootstrapOptions) applyDefaults() {
	if o.Format == "" {
		o.Format = "json"
	}
	if !o.AddCaller && o.CallerSkip == 0 {
		o.AddCaller = true
	}
	if o.File.Path != "" {
		if o.File.MaxSizeMB <= 0 {
			o.File.MaxSizeMB = defaultMaxSizeMB
		}
		if o.File.MaxBackups <= 0 {
			o.File.MaxBackups = defaultMaxBackups
		}
		if o.File.MaxAgeDays <= 0 {
			o.File.MaxAgeDays = defaultMaxAgeDays
		}
	}
}

// LevelName 返回级别字符串（trace/debug/...），用于序列化 / 配置回写。
// 注意：函数名避开 Level.String()，因为 Level 本身已在 logger.go 用 toString 处理。
func LevelName(lv Level) string {
	switch lv {
	case LevelTrace:
		return "trace"
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelNotice:
		return "notice"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	case LevelFatal:
		return "fatal"
	default:
		return "info"
	}
}

// ParseLevel 解析字符串级别，未知值返回 LevelInfo。
func ParseLevel(s string) Level {
	switch s {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "notice":
		return LevelNotice
	case "warn", "warning":
		return LevelWarn
	case "err", "error":
		return LevelError
	case "fatal", "panic":
		return LevelFatal
	default:
		return LevelInfo
	}
}

var _ = time.Second // 占位，避免 import 被工具误删
