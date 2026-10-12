package core

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	// ModeWebhook 生产默认出口。
	ModeWebhook = "webhook"
	// ModeDingTalk 保留实现，本期默认不初始化。
	ModeDingTalk = "dingtalk"
)

// Config 五服务共用的告警配置。密钥必须来自环境变量或配置中心。
type Config struct {
	Enabled        bool           `yaml:"enabled" json:"enabled"` // false 时只打日志，禁止出网
	Mode           string         `yaml:"mode" json:"mode"`       // 仅 webhook / dingtalk
	TimeoutSeconds int            `yaml:"timeout_seconds" json:"timeout_seconds"`
	QueueSize      int            `yaml:"queue_size" json:"queue_size"`
	Environment    string         `yaml:"-" json:"-"` // 写入 Event，不来自 YAML
	Service        string         `yaml:"-" json:"-"`
	Webhook        WebhookConfig  `yaml:"webhook" json:"webhook"`
	DingTalk       DingTalkConfig `yaml:"dingtalk" json:"dingtalk"`
	P0             P0Thresholds   `yaml:"p0" json:"p0"` // 数字阈值；0 表示用代码默认值
}

// P0Thresholds 值班会改的数字。未填或 ≤0 时 ApplyDefaults 回落到代码默认，不必改二进制。
type P0Thresholds struct {
	WaitUploadGT           float64 `yaml:"wait_upload_gt" json:"wait_upload_gt"`                       // 待上传，严格大于
	TempGT                 float64 `yaml:"temp_gt" json:"temp_gt"`                                     // 温度℃，严格大于
	SDRemainMBLT           float64 `yaml:"sd_remain_mb_lt" json:"sd_remain_mb_lt"`                     // SD 剩余 MB，严格小于
	BatteryJump            float64 `yaml:"battery_jump" json:"battery_jump"`                           // 电量相邻跳变百分点
	MQProducerPendingSec   int     `yaml:"mq_producer_pending_sec" json:"mq_producer_pending_sec"`     // 生产通道最老等待秒
	HeartbeatPairWindowSec int     `yaml:"heartbeat_pair_window_sec" json:"heartbeat_pair_window_sec"` // 连续/相邻时间窗秒
	ReconnectWindowSec     int     `yaml:"reconnect_window_sec" json:"reconnect_window_sec"`           // 重连统计窗秒
	ReconnectCount         int     `yaml:"reconnect_count" json:"reconnect_count"`                     // 窗内重连次数阈值
	InstructionMinCount    int     `yaml:"instruction_min_count" json:"instruction_min_count"`         // 5 分钟内超时指令条数
	InstructionMinOnline   int     `yaml:"instruction_min_online" json:"instruction_min_online"`       // 同时涉及的在线设备台数
	// DeviceDailyFireLimit SD 不足、待上传积压各自的自然日触发上限。按设备号计，东八区切日。
	// 不写或 ≤0 时默认 1。调大即放宽；持续异常不再按 10 分钟提醒重复消耗次数。
	DeviceDailyFireLimit int `yaml:"device_daily_fire_limit" json:"device_daily_fire_limit"`
}

// ApplyDefaults 把未配置项补成当前代码默认，避免 yaml 漏字段把阈值变成 0。
func (p *P0Thresholds) ApplyDefaults() {
	if p == nil {
		return
	}
	if p.WaitUploadGT <= 0 {
		p.WaitUploadGT = 200
	}
	if p.TempGT <= 0 {
		p.TempGT = 65
	}
	if p.SDRemainMBLT <= 0 {
		p.SDRemainMBLT = 150
	}
	if p.BatteryJump <= 0 {
		p.BatteryJump = 30
	}
	if p.MQProducerPendingSec <= 0 {
		p.MQProducerPendingSec = 30
	}
	if p.HeartbeatPairWindowSec <= 0 {
		p.HeartbeatPairWindowSec = 6 * 60
	}
	if p.ReconnectWindowSec <= 0 {
		p.ReconnectWindowSec = 30 * 60
	}
	if p.ReconnectCount <= 0 {
		p.ReconnectCount = 5
	}
	if p.InstructionMinCount <= 0 {
		p.InstructionMinCount = 10
	}
	if p.InstructionMinOnline <= 0 {
		p.InstructionMinOnline = 3
	}
	if p.DeviceDailyFireLimit <= 0 {
		p.DeviceDailyFireLimit = 1
	}
}

// MQPending 生产通道最老等待阈值。
func (p P0Thresholds) MQPending() time.Duration {
	p.ApplyDefaults()
	return time.Duration(p.MQProducerPendingSec) * time.Second
}

// PairWindow 心跳连续/电量相邻时间窗。
func (p P0Thresholds) PairWindow() time.Duration {
	p.ApplyDefaults()
	return time.Duration(p.HeartbeatPairWindowSec) * time.Second
}

// ReconnectWindow 重连统计时间窗。
func (p P0Thresholds) ReconnectWindow() time.Duration {
	p.ApplyDefaults()
	return time.Duration(p.ReconnectWindowSec) * time.Second
}

// WebhookConfig Webhook 出口配置。
type WebhookConfig struct {
	URL    string `yaml:"url" json:"url"`
	Secret string `yaml:"secret" json:"secret"`
}

// DingTalkConfig 钉钉出口配置（mode=dingtalk 时才校验）。
type DingTalkConfig struct {
	URL     string `yaml:"url" json:"url"`
	Keyword string `yaml:"keyword" json:"keyword"`
	Secret  string `yaml:"secret" json:"secret"`
}

// Timeout 返回 HTTP 超时，非法值回落到 3 秒。
func (c Config) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return DefaultTimeout
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// QueueCap 返回有界队列容量。
func (c Config) QueueCap() int {
	if c.QueueSize <= 0 {
		return DefaultQueueSize
	}
	return c.QueueSize
}

// ResolveEnv 把 ${ENV} 形式的占位符替换为环境变量，禁止把真实密钥写进仓库。
func ResolveEnv(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "${") && strings.HasSuffix(raw, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(raw, "${"), "}")
		return os.Getenv(key)
	}
	return raw
}

// Validate 校验 mode 与当前出口。非法 mode 必须让进程拒绝启动。
func (c Config) Validate() error {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode != ModeWebhook && mode != ModeDingTalk {
		return fmt.Errorf("p0alert: mode 必须是 webhook 或 dingtalk，当前=%q", c.Mode)
	}
	// 关闭时不校验 URL，便于旧配置先启动再灰度打开。
	if !c.Enabled {
		return nil
	}
	switch mode {
	case ModeWebhook:
		if ResolveEnv(c.Webhook.URL) == "" {
			return fmt.Errorf("p0alert: webhook.url 不能为空")
		}
		// secret 可选：空则推送不带 HMAC，接收端自行决定是否验签。
	case ModeDingTalk:
		if ResolveEnv(c.DingTalk.URL) == "" {
			return fmt.Errorf("p0alert: dingtalk.url 不能为空")
		}
	}
	return nil
}
