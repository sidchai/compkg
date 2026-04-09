package alert

import (
	"github.com/sidchai/compkg/pkg/metrics"
)

// Config 告警模块初始化配置
type Config struct {
	ServiceName     string         // 服务名
	DingTalkWebhook string         // 钉钉 Webhook URL（降级地址）
	DingTalkSecret  string         // 钉钉加签密钥（可选）
	Rules           []Rule         // 告警规则
	ConfigProvider  ConfigProvider // 配置提供者（可选，支持热更新）
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

// EvaluateFunc 返回用于 metrics.OnSnapshot 的回调函数
func EvaluateFunc() metrics.SnapshotCallback {
	return func(snapshots map[string]*metrics.Snapshot) {
		if DefaultEngine != nil {
			DefaultEngine.Evaluate(snapshots)
		}
	}
}
