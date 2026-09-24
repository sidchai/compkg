package alert_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert"
)

func TestWebhookSignAndHeaders(t *testing.T) {
	var gotTS, gotSig string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTS = r.Header.Get(alert.HeaderTimestamp)
		gotSig = r.Header.Get(alert.HeaderSignature)
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := alert.NewWebhookSender(srv.URL, "test-secret", time.Second, srv.Client())
	s.Sleep = func(time.Duration) {}
	ev := alert.NewEvent(alert.CodeRecordPipeline, alert.StatusFiring, "录音处理", "录音转码超过30分钟仍未完成", "测试环境", "upload_server_job", "录音任务：a1d8****70eb", time.Date(2026, 9, 11, 14, 30, 25, 0, time.FixedZone("CST", 8*3600)))
	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if alert.Sign("test-secret", gotTS, body) != gotSig {
		t.Fatalf("signature mismatch")
	}
	var m map[string]string
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["labels"]; ok {
		t.Fatal("labels forbidden")
	}
	if m["eventCode"] != alert.CodeRecordPipeline || m["status"] != alert.StatusFiring {
		t.Fatalf("payload %+v", m)
	}
}

func TestWebhookEmptyResponseNoPanic(t *testing.T) {
	s := alert.NewWebhookSender("http://127.0.0.1:1", "s", 50*time.Millisecond, &http.Client{Timeout: 50 * time.Millisecond})
	s.Sleep = func(time.Duration) {}
	s.MaxAttempt = 1
	ev := alert.NewEvent(alert.CodeHostCPU, alert.StatusFiring, "服务器资源", "CPU", "测试", "iot_server", "", time.Now())
	if err := s.Send(context.Background(), ev); err == nil {
		t.Fatal("want error")
	}
}

func TestWebhookNon2xxRetry(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	s := alert.NewWebhookSender(srv.URL, "s", time.Second, srv.Client())
	s.Sleep = func(time.Duration) {}
	s.MaxAttempt = 3
	_ = s.Send(context.Background(), alert.NewEvent(alert.CodeHostCPU, alert.StatusFiring, "服务器资源", "x", "t", "s", "", time.Now()))
	if n.Load() != 3 {
		t.Fatalf("attempts=%d", n.Load())
	}
}

func TestExclusiveSender(t *testing.T) {
	rt, err := alert.New(alert.Config{
		Enabled: true,
		Mode:    alert.ModeWebhook,
		Webhook: alert.WebhookConfig{URL: "http://127.0.0.1:9/hook", Secret: "abc"},
	}, alert.NewMemoryStore(16), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Reporter.SenderName() != alert.ModeWebhook {
		t.Fatalf("sender=%s", rt.Reporter.SenderName())
	}
}

func TestWebhookSecretOptional(t *testing.T) {
	rt, err := alert.New(alert.Config{
		Enabled: true,
		Mode:    alert.ModeWebhook,
		Webhook: alert.WebhookConfig{URL: "http://127.0.0.1:9/hook"},
	}, alert.NewMemoryStore(16), nil)
	if err != nil {
		t.Fatal(err)
	}
	rt.Close()
}

func TestIllegalMode(t *testing.T) {
	_, err := alert.New(alert.Config{Enabled: true, Mode: "feishu", Webhook: alert.WebhookConfig{URL: "http://x", Secret: "y"}}, nil, nil)
	if err == nil {
		t.Fatal("feishu must fail startup")
	}
}

func TestDisabledNoNetwork(t *testing.T) {
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
	}))
	defer srv.Close()
	rt, err := alert.New(alert.Config{
		Enabled: false,
		Mode:    alert.ModeWebhook,
		Webhook: alert.WebhookConfig{URL: srv.URL, Secret: "s"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.Reporter.SenderName() != "nop" {
		t.Fatalf("disabled should use nop, got %s", rt.Reporter.SenderName())
	}
	rt.Enqueue(alert.NewEvent(alert.CodeHostCPU, alert.StatusFiring, "服务器资源", "x", "t", "s", "", time.Now()))
	time.Sleep(50 * time.Millisecond)
	if hit.Load() != 0 {
		t.Fatal("disabled must not hit network")
	}
}

type memPushLog struct {
	n   atomic.Int64
	ok  atomic.Bool
	err atomic.Value
}

func (m *memPushLog) Save(_ context.Context, ev alert.Event, sendOK bool, sendErr string) error {
	_ = ev
	m.n.Add(1)
	m.ok.Store(sendOK)
	m.err.Store(sendErr)
	return nil
}
func (m *memPushLog) EnsureTable(context.Context) error { return nil }

func TestReporterPersistsAfterSend(t *testing.T) {
	hit := atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	snd := alert.NewWebhookSender(srv.URL, "", time.Second, nil)
	r := alert.NewReporter(true, 8, snd, nil)
	log := &memPushLog{}
	r.SetPushLog(log)
	r.Enqueue(alert.NewEvent(alert.CodeHostCPU, alert.StatusFiring, "服务器资源", "x", "t", "s", "DEV123", time.Now()))
	time.Sleep(200 * time.Millisecond)
	r.Close()
	if hit.Load() == 0 {
		t.Fatal("want send")
	}
	if log.n.Load() == 0 {
		t.Fatal("want persist")
	}
	if !log.ok.Load() {
		t.Fatal("want send ok")
	}
}

func TestQueueFullDrop(t *testing.T) {
	alert.ResetQueueDropTotal()
	block := make(chan struct{})
	sender := &blockSender{ch: block}
	r := alert.NewReporter(true, 2, sender, nil)
	defer func() {
		close(block)
		r.Close()
	}()
	ev := alert.NewEvent(alert.CodeHostCPU, alert.StatusFiring, "服务器资源", "x", "t", "s", "", time.Now())
	r.Enqueue(ev)
	r.Enqueue(ev)
	r.Enqueue(ev)
	r.Enqueue(ev)
	if alert.QueueDropTotal() == 0 {
		t.Fatal("want drops")
	}
}

type blockSender struct{ ch chan struct{} }

func (b *blockSender) Name() string { return "block" }
func (b *blockSender) Send(ctx context.Context, event alert.Event) error {
	select {
	case <-b.ch:
	case <-ctx.Done():
	}
	return nil
}

func TestDingTalkFormatOmitsEmptyObject(t *testing.T) {
	ev := alert.NewEvent(alert.CodeHostCPU, alert.StatusFiring, "服务器资源", "服务器CPU使用率连续5分钟超过90%，当前91%", "生产环境", "iot_server", "", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	txt := alert.FormatDingTalk(ev)
	if strings.Contains(txt, "对象") {
		t.Fatalf("empty object must omit line: %s", txt)
	}
	ev.Object = "磁盘 /data"
	txt = alert.FormatDingTalk(ev)
	if !strings.Contains(txt, "对象：磁盘 /data") {
		t.Fatal(txt)
	}
}

func TestObjectPlaintextInPayload(t *testing.T) {
	raw := "ABCDEFGH12345678"
	obj := alert.MaskObject(raw)
	if obj != raw {
		t.Fatalf("object must be plaintext, got %s", obj)
	}
	body, _ := json.Marshal(alert.OutboundEvent(alert.NewEvent(alert.CodeDeviceHealth, alert.StatusFiring, "设备状态", "x", "t", "iot_server", obj, time.Now())))
	s := string(body)
	if !strings.Contains(s, raw) {
		t.Fatal("want plaintext device in payload")
	}
	if strings.Contains(s, "****") {
		t.Fatal("object must not be masked")
	}
}
