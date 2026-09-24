// Package sender 实现互斥的 Webhook / 钉钉适配器，业务不得同时初始化两个。
package sender

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

const (
	HeaderTimestamp = "X-Alert-Timestamp"
	HeaderSignature = "X-Alert-Signature"
)

// Webhook 按文档 6.3 发送 JSON，HMAC-SHA256 签名。
type Webhook struct {
	url        string
	secret     string
	client     *http.Client
	now        func() time.Time
	Sleep      func(time.Duration)
	backoffs   []time.Duration
	MaxAttempt int
}

// NewWebhook 创建 Webhook 出口。timeout 默认 3 秒。
func NewWebhook(url, secret string, timeout time.Duration, client *http.Client) *Webhook {
	if timeout <= 0 {
		timeout = core.DefaultTimeout
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	} else if client.Timeout == 0 {
		client.Timeout = timeout
	}
	return &Webhook{
		url:        url,
		secret:     secret,
		client:     client,
		now:        time.Now,
		Sleep:      time.Sleep,
		backoffs:   []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
		MaxAttempt: 4,
	}
}

func (s *Webhook) Name() string { return core.ModeWebhook }

// Sign 计算 HMAC-SHA256(secret, timestamp+"."+body)。接收端必须用同一串校验。
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Webhook) Send(ctx context.Context, event core.Event) error {
	payload := OutboundEvent(event)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("p0alert webhook marshal: %w", err)
	}
	// 首次失败后再按 1s/5s/30s 重试，最多 3 次；失败不回滚业务。
	var last error
	for i := 0; i < s.MaxAttempt; i++ {
		if i > 0 {
			wait := s.backoffs[i-1]
			if wait > 0 && s.Sleep != nil {
				s.Sleep(wait)
			}
		}
		last = s.doOnce(ctx, body)
		if last == nil {
			return nil
		}
	}
	return last
}

func (s *Webhook) doOnce(ctx context.Context, body []byte) error {
	ts := strconv.FormatInt(s.now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("p0alert webhook new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderTimestamp, ts)
	// 未配置 secret 时不签，避免用空密钥算出误导接收端的签名。
	if s.secret != "" {
		req.Header.Set(HeaderSignature, Sign(s.secret, ts, body))
	}

	resp, err := s.client.Do(req)
	// 先判空再 Close，避免对端无响应时 panic。
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("p0alert webhook do: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("p0alert webhook empty response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("p0alert webhook status=%d", resp.StatusCode)
	}
	return nil
}

// OutboundEvent 只输出文档 6.3 字段，禁止嵌套 labels/details。
func OutboundEvent(event core.Event) map[string]string {
	m := map[string]string{
		"eventId":     event.EventID,
		"eventCode":   event.EventCode,
		"level":       event.Level,
		"status":      event.Status,
		"scene":       event.Scene,
		"phenomenon":  event.Phenomenon,
		"occurredAt":  event.OccurredAt,
		"environment": event.Environment,
		"service":     event.Service,
	}
	if event.Object != "" {
		m["object"] = event.Object
	}
	return m
}
