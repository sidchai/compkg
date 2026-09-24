package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert"
	"github.com/sidchai/compkg/pkg/alert/metricrule"
)

func TestWebhookSendsSameJSON(t *testing.T) {
	var body []byte
	var gotTS, gotSig string
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTS = r.Header.Get(alert.HeaderTimestamp)
		gotSig = r.Header.Get(alert.HeaderSignature)
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		close(done)
	}))
	defer srv.Close()

	n := NewWebhook(srv.URL, "test-secret", "测试环境", time.Second, 8)
	if n == nil {
		t.Fatal("expected notifier")
	}
	defer n.Close()

	err := n.Send(metricrule.AlertEvent{
		ServiceName: "audio_self_host",
		MetricName:  "init_fail",
		Level:       metricrule.LevelP0,
		Title:       "初始化失败",
		Message:     "当前值1，阈值0",
		Timestamp:   time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not received")
	}
	if alert.Sign("test-secret", gotTS, body) != gotSig {
		t.Fatal("signature mismatch")
	}
	var m map[string]string
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m["eventCode"] != metricrule.EventCodeMetricPrefix+"init_fail" || m["status"] != alert.StatusFiring {
		t.Fatalf("payload %+v", m)
	}
}

func TestNewWebhookAllowsEmptySecret(t *testing.T) {
	n := NewWebhook("http://127.0.0.1/hook", "", "t", time.Second, 1)
	if n == nil {
		t.Fatal("empty secret should still create webhook")
	}
	n.Close()
}

func TestNewWebhookRequiresURL(t *testing.T) {
	if n := NewWebhook("", "s", "t", time.Second, 1); n != nil {
		t.Fatal("empty url must skip webhook")
	}
}
