package metricrule

import (
	"github.com/sidchai/compkg/pkg/metrics"
)

// Config 告警模块初始化配置
type Config struct {
	ServiceName     string         // 服务名
	Environment     string         // 调用方传给 Webhook Notifier，不进钉钉 Markdown
	DingTalkWebhook string         // 钉钉 Webhook URL（降级地址，由调用方挂 Notifier）
	DingTalkSecret  string         // 钉钉加签密钥（可选）
	Rules           []Rule         // 告警规则
	ConfigProvider  ConfigProvider // 配置提供者（可选，支持热更新钉钉）
	Store           AlertStore     // 告警持久化（可选）
}

// DefaultEngine 全局默认告警引擎
var DefaultEngine *Engine

// Init 初始化全局告警引擎
func Init(cfg Config) {
	opts := make([]EngineOption, 0)
	if cfg.ConfigProvider != nil {
		opts = append(opts, WithConfigProvider(cfg.ConfigProvider))
	}
	if cfg.Store != nil {
		opts = append(opts, WithStore(cfg.Store))
	}
	DefaultEngine = NewEngine(cfg.ServiceName, cfg.Rules, opts...)
}

// Close 停掉 Webhook 发送协程。钉钉 Notifier 无后台协程可忽略。
func Close() {
	if DefaultEngine != nil {
		DefaultEngine.Close()
	}
}

// EvaluateFunc 返回用于 metrics.OnSnapshot 的回调函数
func EvaluateFunc() metrics.SnapshotCallback {
	return func(snapshots map[string]*metrics.Snapshot) {
		if DefaultEngine != nil {
			DefaultEngine.Evaluate(snapshots)
		}
	}
}
