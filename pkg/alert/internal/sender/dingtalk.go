package sender

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// DingTalk 把统一事件转成文档 6.4 的固定中文文本。本期默认不初始化。
type DingTalk struct {
	url    string
	secret string
	client *http.Client
}

// NewDingTalk 仅在 mode=dingtalk 时调用。
func NewDingTalk(rawURL, secret string, timeout time.Duration, client *http.Client) *DingTalk {
	if timeout <= 0 {
		timeout = core.DefaultTimeout
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &DingTalk{url: rawURL, secret: secret, client: client}
}

func (s *DingTalk) Name() string { return core.ModeDingTalk }

// Format 按文档 6.4 固定中文模板。object 为空必须整行省略，禁止打出空值。
func Format(event core.Event) string {
	kind := "告警"
	if event.Status == core.StatusResolved {
		kind = "恢复"
	}
	occurred := event.OccurredAt
	if t, err := time.Parse(time.RFC3339, event.OccurredAt); err == nil {
		occurred = t.Format("2006-01-02 15:04:05")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【%s%s】%s\n", event.Level, kind, event.Scene)
	fmt.Fprintf(&b, "现象：%s\n", event.Phenomenon)
	fmt.Fprintf(&b, "时间：%s\n", occurred)
	fmt.Fprintf(&b, "环境：%s\n", event.Environment)
	fmt.Fprintf(&b, "服务：%s", event.Service)
	if event.Object != "" {
		fmt.Fprintf(&b, "\n对象：%s", event.Object)
	}
	return b.String()
}

func (s *DingTalk) Send(ctx context.Context, event core.Event) error {
	webhookURL := s.url
	if s.secret != "" {
		webhookURL = signDingTalkURL(webhookURL, s.secret, time.Now().UnixMilli())
	}
	body := map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": Format(event)},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("p0alert dingtalk marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("p0alert dingtalk new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("p0alert dingtalk do: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("p0alert dingtalk empty response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("p0alert dingtalk status=%d", resp.StatusCode)
	}
	return nil
}

func signDingTalkURL(rawURL, secret string, ts int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", ts, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + "timestamp=" + strconv.FormatInt(ts, 10) + "&sign=" + sign
}
