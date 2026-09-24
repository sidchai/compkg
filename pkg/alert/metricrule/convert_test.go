package metricrule

import (
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert"
)

func TestToEventMatchesOutboundShape(t *testing.T) {
	src := AlertEvent{
		ServiceName: "iot_server",
		MetricName:  "disk_usage_percent",
		Level:       LevelP1,
		Title:       "磁盘使用率过高",
		Message:     "服务[iot_server] 指标[disk_usage_percent] 当前值: 92.00, 阈值: 85.00",
		Value:       92.0,
		Threshold:   85.0,
		Tags:        map[string]string{"device_no": "ABCDEFGH12345678", "url": "http://x"},
		Timestamp:   time.Date(2026, 9, 16, 15, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	}
	ev := ToEvent(src, "测试环境")
	m := alert.OutboundEvent(ev)
	if _, ok := m["labels"]; ok {
		t.Fatal("labels forbidden")
	}
	if _, ok := m["metricName"]; ok {
		t.Fatal("metricName must not appear in webhook json")
	}
	if _, ok := m["tags"]; ok {
		t.Fatal("tags must not appear in webhook json")
	}
	if m["object"] != "" {
		t.Fatalf("device tags must not become object: %+v", m)
	}
	if m["eventCode"] != EventCodeMetricPrefix+"disk_usage_percent" {
		t.Fatalf("eventCode=%s", m["eventCode"])
	}
	if m["level"] != "P1" {
		t.Fatalf("level=%s", m["level"])
	}
	if m["status"] != alert.StatusFiring {
		t.Fatalf("status=%s", m["status"])
	}
	if m["scene"] != "磁盘使用率过高" || m["service"] != "iot_server" || m["environment"] != "测试环境" {
		t.Fatalf("payload %+v", m)
	}
	if m["phenomenon"] == "" || m["eventId"] == "" || m["occurredAt"] == "" {
		t.Fatalf("missing required fields %+v", m)
	}
}
