package alert

import (
	"time"
)

// Level 告警级别
type Level int

const (
	LevelP0 Level = 0 // 立即处理
	LevelP1 Level = 1 // 一周内
	LevelP2 Level = 2 // 后续
)

// LevelText 告警级别文本
func (l Level) Text() string {
	switch l {
	case LevelP0:
		return "P0"
	case LevelP1:
		return "P1"
	case LevelP2:
		return "P2"
	default:
		return "UNKNOWN"
	}
}

// LevelEmoji 告警级别 emoji
func (l Level) Emoji() string {
	switch l {
	case LevelP0:
		return "🔴"
	case LevelP1:
		return "🟠"
	case LevelP2:
		return "🟡"
	default:
		return "⚪"
	}
}

// AlertEvent 告警事件
type AlertEvent struct {
	ServiceName string            `json:"serviceName"` // 服务名
	MetricName  string            `json:"metricName"`  // 埋点名
	Level       Level             `json:"level"`       // 告警级别
	Title       string            `json:"title"`       // 告警标题
	Message     string            `json:"message"`     // 告警详情
	Value       interface{}       `json:"value"`       // 当前值
	Threshold   interface{}       `json:"threshold"`   // 阈值
	Tags        map[string]string `json:"tags"`        // 标签
	Timestamp   time.Time         `json:"timestamp"`   // 时间
}

// NotifierConfig 通知配置（支持热更新）
type NotifierConfig struct {
	DingTalkWebhook string `json:"dingtalk_webhook"` // 钉钉 Webhook URL
	DingTalkSecret  string `json:"dingtalk_secret"`  // 钉钉加签密钥（可选）
	Enabled         bool   `json:"enabled"`          // 是否启用告警
}

// AlertStore 告警持久化接口
type AlertStore interface {
	Save(event AlertEvent) error
}
