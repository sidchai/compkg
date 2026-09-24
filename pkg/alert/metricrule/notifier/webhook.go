package notifier

import (
	"time"

	"github.com/sidchai/compkg/pkg/alert"
	"github.com/sidchai/compkg/pkg/alert/metricrule"
)

var _ metricrule.Notifier = (*Webhook)(nil)

// Webhook 把 YAML 告警异步推到与 P0 相同的 Webhook 出口。
// Send 只入队，禁止在 metrics 快照回调里同步 HTTP（重试最长约 90 秒）。
type Webhook struct {
	reporter *alert.Reporter
	env      string
}

// NewWebhook 只要求 URL。secret 为空仍创建，推送时不带 HMAC。
func NewWebhook(url, secret, env string, timeout time.Duration, queueSize int) *Webhook {
	url = alert.ResolveEnv(url)
	secret = alert.ResolveEnv(secret)
	if url == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	snd := alert.NewWebhookSender(url, secret, timeout, nil)
	return &Webhook{
		reporter: alert.NewReporter(true, queueSize, snd, nil),
		env:      env,
	}
}

// Send 实现 metricrule.Notifier。队列满时由 Reporter 丢弃并计数，不返回业务错误。
func (w *Webhook) Send(event metricrule.AlertEvent) error {
	if w == nil || w.reporter == nil {
		return nil
	}
	w.reporter.Enqueue(metricrule.ToEvent(event, w.env))
	return nil
}

// SetPushLog 旧 YAML webhook 与 P0 共用流水表。须在 AddNotifier 前调用。
func (w *Webhook) SetPushLog(s alert.PushLogStore) {
	if w == nil || w.reporter == nil {
		return
	}
	w.reporter.SetPushLog(s)
}

// Close 等待发送协程退出。重复调用由 Reporter 保证安全。
func (w *Webhook) Close() {
	if w == nil || w.reporter == nil {
		return
	}
	w.reporter.Close()
}
