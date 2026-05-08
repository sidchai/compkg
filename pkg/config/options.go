// Package config 提供基于 Nacos + 本地 fallback 的配置中心 SDK。
//
// 加载顺序：本地 yaml fallback -> Remote(Nacos) override -> 环境变量展开 -> ENC 解密 -> 触发 listeners。
//
// 与 RFC-06 配套，详见 docs/rfc/06_config_center.md。
package config

import (
	"context"
	"time"
)

// BootstrapOptions Loader 启动选项。
type BootstrapOptions struct {
	// ServiceName 服务名（用于日志、灰度上下文），如 "service.iot_cloud_platform_server"。
	ServiceName string

	// Namespace 环境，对应 Nacos namespace：dev / test / prod / private。
	Namespace string

	// LocalPath 本地 fallback yaml 文件绝对/相对路径。Nacos 不可达时仍可用此启动。
	// 必填——保证最小可用。
	LocalPath string

	// LocalConfigType 本地配置文件类型，默认 "yaml"。
	LocalConfigType string

	// Remote 可选远程配置源（如 Nacos）。nil 时仅本地启动，不订阅热更新。
	Remote RemoteSource

	// EncryptKeyPath 解密 ENC(...) 的密钥所在的 dot-key 路径；
	// 密钥从合并后的配置（本地 + Nacos）中读取，支持 string 或 []string：
	//
	//	config:
	//	  encryptKey: <base64-16字节>           # 单密钥
	//	# 或
	//	config:
	//	  encryptKey:                            # 轮换期多密钥，依次尝试解密
	//	    - <base64-key1>
	//	    - <base64-key2>
	//	# 或字符串以英文逗号分隔：
	//	config:
	//	  encryptKey: "<key1>,<key2>"
	//
	// 默认值 "config.encryptKey"。配置中无该 key 时，若 ENC(...) 不存在则正常启动；
	// 存在 ENC(...) 但无密钥则报错拒绝启动。
	//
	// ⚠️ 安全注意：encryptKey 本身在配置中明文存放（非 ENC()），需控制配置文件读权限。
	EncryptKeyPath string

	// EnvWhitelist 允许 ${VAR} 展开的环境变量白名单（避免任意变量泄漏到配置）。
	// 为空时使用默认白名单：POD_IP / HOSTNAME / VERSION / NODE_NAME / NAMESPACE。
	EnvWhitelist []string

	// HotReloadable 标记 dot-key 哪些配置允许热更新。
	// 不在此白名单内的字段变更只记录日志、提示运维重启，不触发 listeners。
	// 为空时默认所有变更都触发 listeners（开发模式）。
	HotReloadable []string

	// FetchTimeout 单次拉取/订阅超时，默认 5s。
	FetchTimeout time.Duration

	// Schema JSON Schema（draft-07/2020-12）原始字节。
	// 启动时在 env 展开 + ENC 解密完成后做最终校验：
	//   - 启动校验失败 → 返回 error，拒绝启动
	//   - 热更校验失败 → 沿用旧配置，仅记录 error 日志，并触发 config_validation_failure_total
	//
	// 为空时跳过校验（不推荐生产环境）。
	Schema []byte

	// Logger 可选日志钩子。若为 nil，仅打到 stderr。
	Logger LoggerFunc
}

// LoggerFunc SDK 内部日志输出钩子，避免硬绑死某个日志库。
//
//	level: "info" / "warn" / "error"
//	msg:   描述
//	kv:    key/value pairs，奇数 key 偶数 value
type LoggerFunc func(level, msg string, kv ...any)

// RemoteSource 远程配置源抽象（典型实现：Nacos）。
// 多源并存留待后续扩展（如 Redis L2 兜底）。
type RemoteSource interface {
	// Name 唯一名称，用于日志和指标。
	Name() string

	// Fetch 启动时一次性拉取所有配置，按 dataId -> raw yaml bytes 返回。
	// dataId 命名见 RFC-06 §dataId 规范。
	Fetch(ctx context.Context) (map[string][]byte, error)

	// Listen 订阅变更。当远端某 dataId 发生变更时调用 onChange(dataId, rawYaml)。
	// 返回的 cancel 用于关闭订阅。
	Listen(ctx context.Context, onChange func(dataId string, raw []byte)) (cancel func() error, err error)
}

// FeatureContext 灰度上下文。
type FeatureContext struct {
	DeviceNo   string
	AccountId  int64
	InstanceId string
}

// FeatureFlag 单个开关的 schema，对应 feature.flags.yaml 中每一项。
type FeatureFlag struct {
	Enabled      bool    `yaml:"enabled" mapstructure:"enabled"`
	Rollout      int     `yaml:"rollout" mapstructure:"rollout"`             // 0~100
	SamplingRate float64 `yaml:"sampling_rate" mapstructure:"sampling_rate"` // 0.0~1.0，备用
}
