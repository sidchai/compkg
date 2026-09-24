// Package reporter 用有界队列把发送从业务线程剥离。
package reporter

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// queueDropTotal 对应指标 alert_reporter_queue_drop_total，仅自检。
var queueDropTotal atomic.Int64

// QueueDropTotal 返回进程内丢弃次数，供监控自检。
func QueueDropTotal() int64 { return queueDropTotal.Load() }

// ResetQueueDropTotal 仅单测使用，禁止业务调用。
func ResetQueueDropTotal() { queueDropTotal.Store(0) }

// Reporter 有界异步队列。业务只 Enqueue，独立协程发送。
type Reporter struct {
	enabled bool
	sender  core.Sender
	ch      chan core.Event
	logger  *slog.Logger
	wg      sync.WaitGroup
	closed  atomic.Bool
	pushMu  sync.Mutex
	pushLog core.PushLogStore
}

// New 启动独立发送协程。sender 必须已按 mode 互斥创建。
func New(enabled bool, queueSize int, sender core.Sender, logger *slog.Logger) *Reporter {
	if queueSize <= 0 {
		queueSize = core.DefaultQueueSize
	}
	if sender == nil {
		sender = core.NopSender{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &Reporter{
		enabled: enabled,
		sender:  sender,
		ch:      make(chan core.Event, queueSize),
		logger:  logger,
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

func (r *Reporter) loop() {
	defer r.wg.Done()
	for ev := range r.ch {
		if !r.enabled {
			r.logger.Info("p0alert disabled, skip send",
				"eventId", ev.EventID, "eventCode", ev.EventCode, "status", ev.Status)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		err := r.sender.Send(ctx, ev)
		cancel()
		if err != nil {
			r.logger.Error("p0alert send failed",
				"eventId", ev.EventID, "eventCode", ev.EventCode, "sender", r.sender.Name(), "err", err)
			r.persist(ev, false, err.Error())
			continue
		}
		r.logger.Info("p0alert sent",
			"eventId", ev.EventID, "eventCode", ev.EventCode, "status", ev.Status, "sender", r.sender.Name())
		r.persist(ev, true, "")
	}
}

// SetPushLog 绑定流水存储。须在首条 Enqueue 前调用；写库失败不影响发送。
func (r *Reporter) SetPushLog(s core.PushLogStore) {
	if r == nil {
		return
	}
	r.pushMu.Lock()
	r.pushLog = s
	r.pushMu.Unlock()
}

func (r *Reporter) persist(ev core.Event, sendOK bool, sendErr string) {
	r.pushMu.Lock()
	s := r.pushLog
	r.pushMu.Unlock()
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Save(ctx, ev, sendOK, sendErr); err != nil {
		r.logger.Error("p0alert persist push log failed",
			"eventId", ev.EventID, "eventCode", ev.EventCode, "err", err)
	}
}

// Enqueue 非阻塞入队。满则丢并计数，禁止 select 阻塞业务。
func (r *Reporter) Enqueue(ev core.Event) {
	if r == nil || r.closed.Load() {
		return
	}
	if ev.EventID == "" {
		ev.EventID = core.NewEvent(ev.EventCode, ev.Status, ev.Scene, ev.Phenomenon, ev.Environment, ev.Service, ev.Object, time.Now()).EventID
	}
	select {
	case r.ch <- ev:
	default:
		queueDropTotal.Add(1)
		r.logger.Error("p0alert queue full, drop",
			"eventCode", ev.EventCode, "dropped", queueDropTotal.Load())
	}
}

// SenderName 用于单测证明同一 Runtime 只挂一个出口。
func (r *Reporter) SenderName() string {
	if r == nil || r.sender == nil {
		return ""
	}
	return r.sender.Name()
}

// Close 关闭 channel 并等待发送协程退出；重复调用安全。
func (r *Reporter) Close() {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return
	}
	close(r.ch)
	r.wg.Wait()
}

// ReportLegacy 承接旧 SendAlert(string)，截断正文以免把整段回调打进 webhook。
func (r *Reporter) ReportLegacy(env, service, msg string) {
	if r == nil {
		return
	}
	ev := core.NewEvent(core.EventLegacyNotify, core.StatusFiring, "历史通知", clipLegacy(msg), env, service, "", time.Now())
	r.Enqueue(ev)
}

func clipLegacy(msg string) string {
	const max = 500
	msg = strings.TrimSpace(msg)
	if len(msg) <= max {
		return msg
	}
	return msg[:max] + "..."
}
