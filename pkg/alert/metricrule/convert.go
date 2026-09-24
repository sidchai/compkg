package metricrule

import (
	"fmt"
	"strings"

	"github.com/sidchai/compkg/pkg/alert"
)

// EventCodeMetricPrefix 旧 YAML 规则的 eventCode 前缀。
// 与 18 个 P0 编码隔离，避免同一故障被接收端当成新 P0 去重。
const EventCodeMetricPrefix = "metric."

// ToEvent 把 YAML 引擎事件转成文档 6.3 字段。
// 禁止把 tags / metricName / 设备号写进 Webhook JSON；设备相关标签直接丢弃。
func ToEvent(src AlertEvent, env string) alert.Event {
	code := EventCodeMetricPrefix + strings.TrimSpace(src.MetricName)
	if strings.TrimSpace(src.MetricName) == "" {
		code = EventCodeMetricPrefix + "unknown"
	}
	scene := strings.TrimSpace(src.Title)
	if scene == "" {
		scene = "系统监控"
	}
	phenomenon := strings.TrimSpace(src.Message)
	if phenomenon == "" {
		phenomenon = fmt.Sprintf("当前值%s，阈值%s", formatMetricValue(src.Value), formatMetricValue(src.Threshold))
	}
	at := src.Timestamp
	ev := alert.NewEvent(code, alert.StatusFiring, scene, phenomenon, env, src.ServiceName, "", at)
	// NewEvent 默认 P0；YAML 仍有 P1/P2，只改 level 字符串，JSON 字段集合不变。
	if text := src.Level.Text(); text != "" && text != "UNKNOWN" {
		ev.Level = text
	}
	return ev
}

func formatMetricValue(v interface{}) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%.2f", val)
	case float32:
		return fmt.Sprintf("%.2f", val)
	default:
		return fmt.Sprintf("%v", v)
	}
}
