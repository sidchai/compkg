package alert

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
	"github.com/sidchai/compkg/pkg/alert/internal/device"
	"github.com/sidchai/compkg/pkg/alert/internal/engine"
	"github.com/sidchai/compkg/pkg/alert/internal/pipeline"
	"github.com/sidchai/compkg/pkg/alert/internal/reporter"
	"github.com/sidchai/compkg/pkg/alert/internal/sender"
	"github.com/sidchai/compkg/pkg/alert/internal/store"
)

// Runtime 进程内装配根。一个进程只应 New 一次，避免双 Sender、双队列。
type Runtime struct {
	Reporter   *reporter.Reporter
	Evaluator  *engine.Evaluator
	Store      core.StateStore
	Pipeline   core.PipelineTracker
	Device     *device.Engine
	Suppressor *engine.Suppressor
	Ingester   *BoundedIngester
	cfg        core.Config
}

// New 按配置初始化。mode 非法或当前出口缺密钥时返回 error，调用方应拒绝启动。
func New(cfg Config, st StateStore, logger *slog.Logger) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	snd, err := newExclusiveSender(cfg)
	if err != nil {
		return nil, err
	}
	// Redis 未注入时降级本机，保证监控失败不堵启动。
	if st == nil {
		st = store.NewMemoryStore(0)
	}
	cfg.P0.ApplyDefaults()
	dev := device.New(0, nil)
	dev.SetThresholds(cfg.P0)
	// 自然日次数跨进程、跨重启仍要生效，必须走同一份 StateStore。
	dev.SetStore(st)
	rt := &Runtime{
		Reporter:   reporter.New(cfg.Enabled, cfg.QueueCap(), snd, logger),
		Evaluator:  engine.NewEvaluator(engine.RuleSpec{FireConsecutive: 1, RecoverConsecutive: 1}),
		Store:      st,
		Pipeline:   pipeline.New(st, logger),
		Device:     dev,
		Suppressor: engine.NewSuppressor(),
		cfg:        cfg,
	}
	return rt, nil
}

// P0 返回已补默认值的数字阈值，供 MQ 等本服务循环读取。
func (rt *Runtime) P0() P0Thresholds {
	if rt == nil {
		var z P0Thresholds
		z.ApplyDefaults()
		return z
	}
	return rt.cfg.P0
}

// newExclusiveSender 按 mode 只创建一条出口，用于证明不会钉钉+Webhook 双发。
func newExclusiveSender(cfg Config) (core.Sender, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if !cfg.Enabled {
		return core.NopSender{}, nil
	}
	switch mode {
	case core.ModeWebhook:
		return sender.NewWebhook(core.ResolveEnv(cfg.Webhook.URL), core.ResolveEnv(cfg.Webhook.Secret), cfg.Timeout(), nil), nil
	case core.ModeDingTalk:
		return sender.NewDingTalk(core.ResolveEnv(cfg.DingTalk.URL), core.ResolveEnv(cfg.DingTalk.Secret), cfg.Timeout(), nil), nil
	default:
		return nil, fmt.Errorf("p0alert: 未创建 Sender")
	}
}

// Close 先停摄入再关发送协程，避免关闭后再 Enqueue。
func (rt *Runtime) Close() {
	if rt == nil {
		return
	}
	if rt.Ingester != nil {
		rt.Ingester.Close()
	}
	rt.Reporter.Close()
}

// SetPushLog 把推送流水接到发送协程。store 为空则只推不落库。
func (rt *Runtime) SetPushLog(s PushLogStore) {
	if rt == nil || rt.Reporter == nil {
		return
	}
	rt.Reporter.SetPushLog(s)
}

// Enqueue 业务热路径入口：非阻塞，满则丢。
func (rt *Runtime) Enqueue(ev Event) {
	if rt == nil || rt.Reporter == nil {
		return
	}
	rt.Reporter.Enqueue(ev)
}
