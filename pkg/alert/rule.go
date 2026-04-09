package alert

import (
	"time"
)

// RuleType 告警规则类型
type RuleType int

const (
	RuleTypeThreshold   RuleType = 1 // 阈值：value > threshold
	RuleTypeRate        RuleType = 2 // 比率：fail_rate > threshold
	RuleTypeSurge       RuleType = 3 // 突变：变化幅度 > percentage (0-100)
	RuleTypeConsecutive RuleType = 4 // 连续N次：连续 N 个周期满足条件
	RuleTypeImmediate   RuleType = 5 // 立即：value > 0 即告警
)

// RuleTypeText 规则类型文本
func (r RuleType) Text() string {
	switch r {
	case RuleTypeThreshold:
		return "threshold"
	case RuleTypeRate:
		return "rate"
	case RuleTypeSurge:
		return "surge"
	case RuleTypeConsecutive:
		return "consecutive"
	case RuleTypeImmediate:
		return "immediate"
	default:
		return "unknown"
	}
}

// ParseRuleType 从字符串解析规则类型
func ParseRuleType(s string) RuleType {
	switch s {
	case "threshold":
		return RuleTypeThreshold
	case "rate":
		return RuleTypeRate
	case "surge":
		return RuleTypeSurge
	case "consecutive":
		return RuleTypeConsecutive
	case "immediate":
		return RuleTypeImmediate
	default:
		return RuleTypeThreshold
	}
}

// Rule 告警规则
type Rule struct {
	MetricName     string        `json:"metric" yaml:"metric"`               // 对应的埋点名
	RuleType       RuleType      `json:"type" yaml:"type"`                   // 规则类型
	Level          Level         `json:"level" yaml:"level"`                 // 告警级别
	Title          string        `json:"title" yaml:"title"`                 // 告警标题
	Threshold      float64       `json:"threshold" yaml:"threshold"`         // 阈值/比率/百分比
	ConsecutiveN   int           `json:"consecutive_n" yaml:"consecutive_n"` // 连续N个周期（用于 RuleTypeConsecutive）
	MinSamples     int64         `json:"min_samples" yaml:"min_samples"`     // rate类型最小样本量（低于此值不判定失败率，防止小样本误告警）
	CooldownPeriod time.Duration `json:"cooldown" yaml:"cooldown"`           // 冷却时间
}

// defaultMinSamples rate 类型规则的默认最小样本量
const defaultMinSamples int64 = 5
