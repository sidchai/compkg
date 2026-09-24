package core

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 推送状态只允许 FIRING / RESOLVED，与文档 6.3 一致。
const (
	StatusFiring   = "FIRING"
	StatusResolved = "RESOLVED"
	LevelP0        = "P0"

	// EventLegacyNotify 承接旧 NotifyHook 文本，不另扩第三出口。
	EventLegacyNotify = "legacy.notify"
)

// Event 统一告警事件。业务不得感知钉钉或 Webhook 报文。
type Event struct {
	EventID     string `json:"eventId"`    // 本次推送幂等 ID
	EventCode   string `json:"eventCode"`  // 稳定编码，接收端按此识别场景
	Level       string `json:"level"`      // 本期固定 P0
	Status      string `json:"status"`     // 只允许 FIRING / RESOLVED
	Scene       string `json:"scene"`      // 值班可读场景，禁止内部术语
	Phenomenon  string `json:"phenomenon"` // 必须含实际值、阈值或持续时间之一
	OccurredAt  string `json:"occurredAt"` // RFC3339 且带时区
	Environment string `json:"environment"`
	Service     string `json:"service"`
	Object      string `json:"object,omitempty"` // 告警对象明文，无则省略

	// DedupKey 仅内存/状态机使用，禁止写入 Webhook JSON。
	DedupKey string `json:"-"`
	// Stage / Reason 仅结构化日志使用。
	Stage  string `json:"-"`
	Reason string `json:"-"`
}

// NewEvent 构造一次推送。occurredAt 使用带时区的 ISO8601。
func NewEvent(code, status, scene, phenomenon, env, service, object string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now()
	}
	ev := Event{
		EventID:     uuid.NewString(),
		EventCode:   code,
		Level:       LevelP0,
		Status:      status,
		Scene:       scene,
		Phenomenon:  phenomenon,
		OccurredAt:  at.Format(time.RFC3339),
		Environment: env,
		Service:     service,
		Object:      strings.TrimSpace(object),
	}
	return ev
}

// Dedup 生成去重键：环境 + eventCode + 附加段。设备号必须先哈希。
func Dedup(parts ...string) string {
	return strings.Join(parts, "|")
}

// HashID 对业务标识做稳定哈希，禁止把明文设备号/任务号当 Prometheus 标签。
func HashID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

// MaskObject 现按需求返回明文（去空白）。Prometheus 标签仍禁止明文，用 HashID。
func MaskObject(raw string) string {
	return strings.TrimSpace(raw)
}
