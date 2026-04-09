package notifier

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/sidchai/compkg/pkg/alert"
)

// DingTalkOption 钉钉通知选项
type DingTalkOption func(*DingTalkNotifier)

// WithSecret 设置加签密钥
func WithSecret(secret string) DingTalkOption {
	return func(d *DingTalkNotifier) {
		d.secret = secret
	}
}

// WithTimeout 设置 HTTP 请求超时
func WithTimeout(timeout time.Duration) DingTalkOption {
	return func(d *DingTalkNotifier) {
		d.client.Timeout = timeout
	}
}

// WithConfigProvider 设置配置提供者（支持热更新 Webhook URL）
func WithConfigProvider(p alert.ConfigProvider) DingTalkOption {
	return func(d *DingTalkNotifier) {
		d.configProvider = p
	}
}

// DingTalkNotifier 钉钉 Webhook 通知器
type DingTalkNotifier struct {
	webhookURL     string
	secret         string
	client         *http.Client
	configProvider alert.ConfigProvider
}

// NewDingTalk 创建钉钉通知器
func NewDingTalk(webhookURL string, opts ...DingTalkOption) *DingTalkNotifier {
	d := &DingTalkNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Send 发送告警通知
func (d *DingTalkNotifier) Send(event alert.AlertEvent) error {
	webhookURL, secret := d.getConfig()
	if webhookURL == "" {
		return fmt.Errorf("dingtalk webhook url is empty")
	}

	// 加签
	if secret != "" {
		webhookURL = d.signURL(webhookURL, secret)
	}

	// 构造 Markdown 消息
	body := d.buildMarkdownBody(event)
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal dingtalk body failed: %w", err)
	}

	resp, err := d.client.Post(webhookURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("send dingtalk request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dingtalk response status: %d, body: %s", resp.StatusCode, string(respBody))
	}

	// 检查钉钉返回结果
	var result dingTalkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		if result.ErrCode != 0 {
			return fmt.Errorf("dingtalk error: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
		}
	}

	return nil
}

func (d *DingTalkNotifier) getConfig() (webhookURL, secret string) {
	// 优先从 ConfigProvider 获取（热更新）
	if d.configProvider != nil {
		cfg, err := d.configProvider.GetNotifierConfig()
		if err == nil && cfg != nil && cfg.DingTalkWebhook != "" {
			return cfg.DingTalkWebhook, cfg.DingTalkSecret
		}
	}
	// 降级使用静态配置
	return d.webhookURL, d.secret
}

func (d *DingTalkNotifier) signURL(webhookURL, secret string) string {
	timestamp := time.Now().UnixMilli()
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(h.Sum(nil)))

	return fmt.Sprintf("%s&timestamp=%d&sign=%s", webhookURL, timestamp, sign)
}

func (d *DingTalkNotifier) buildMarkdownBody(event alert.AlertEvent) map[string]interface{} {
	title := fmt.Sprintf("%s %s告警 - %s", event.Level.Emoji(), event.Level.Text(), event.ServiceName)
	text := fmt.Sprintf(
		"### %s %s告警：%s\n\n"+
			"- **服务**: %s\n"+
			"- **指标**: %s\n"+
			"- **当前值**: %s\n"+
			"- **阈值**: %s\n"+
			"- **时间**: %s\n",
		event.Level.Emoji(),
		event.Level.Text(),
		event.Title,
		event.ServiceName,
		event.MetricName,
		formatAlertValue(event.Value),
		formatAlertValue(event.Threshold),
		event.Timestamp.Format("2006-01-02 15:04:05"),
	)

	if event.Message != "" {
		text += fmt.Sprintf("- **详情**: %s\n", event.Message)
	}

	if len(event.Tags) > 0 {
		text += "- **标签**: "
		for k, v := range event.Tags {
			text += fmt.Sprintf("%s=%s ", k, v)
		}
		text += "\n"
	}

	text += "\n> 请及时处理"

	return map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  text,
		},
	}
}

// formatAlertValue 格式化告警值，float64 保留两位小数避免浮点精度问题
func formatAlertValue(v interface{}) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%.2f", val)
	case float32:
		return fmt.Sprintf("%.2f", val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

type dingTalkResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}
