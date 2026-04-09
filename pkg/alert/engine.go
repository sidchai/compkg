package alert

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/metrics"
)

// Notifier 通知发送接口
type Notifier interface {
	Send(event AlertEvent) error
}

// ConfigProvider 告警配置提供者接口（支持热更新）
// 不设置则使用 Init 时传入的静态配置
type ConfigProvider interface {
	GetNotifierConfig() (*NotifierConfig, error)
}

// EngineOption 引擎选项
type EngineOption func(*Engine)

// WithStore 设置告警持久化
func WithStore(s AlertStore) EngineOption {
	return func(e *Engine) {
		e.store = s
	}
}

// WithConfigProvider 设置配置提供者（支持热更新）
func WithConfigProvider(p ConfigProvider) EngineOption {
	return func(e *Engine) {
		e.configProvider = p
	}
}

// Engine 告警引擎
type Engine struct {
	rules          []Rule
	notifiers      []Notifier
	cooldowns      sync.Map // metricName -> time.Time (上次告警时间)
	prevGauges     sync.Map // metricName -> float64 (上一周期 Gauge 值，用于突变检测)
	prevCounters   sync.Map // metricName -> int64 (上一周期 Counter 值)
	consecutive    sync.Map // metricName -> int (连续满足条件计数)
	store          AlertStore
	configProvider ConfigProvider
	serviceName    string
	mu             sync.RWMutex
}

// NewEngine 创建告警引擎
func NewEngine(serviceName string, rules []Rule, opts ...EngineOption) *Engine {
	e := &Engine{
		rules:       rules,
		notifiers:   make([]Notifier, 0),
		serviceName: serviceName,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// AddNotifier 添加通知渠道
func (e *Engine) AddNotifier(n Notifier) {
	e.mu.Lock()
	e.notifiers = append(e.notifiers, n)
	e.mu.Unlock()
}

// SetStore 设置告警持久化
func (e *Engine) SetStore(s AlertStore) {
	e.mu.Lock()
	e.store = s
	e.mu.Unlock()
}

// SetConfigProvider 设置配置提供者
func (e *Engine) SetConfigProvider(p ConfigProvider) {
	e.mu.Lock()
	e.configProvider = p
	e.mu.Unlock()
}

// UpdateRules 动态更新规则
func (e *Engine) UpdateRules(rules []Rule) {
	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()
}

// Evaluate 评估所有规则（由 metrics OnSnapshot 回调触发）
func (e *Engine) Evaluate(snapshots map[string]*metrics.Snapshot) {
	e.mu.RLock()
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()

	now := time.Now()

	for _, rule := range rules {
		snap, ok := snapshots[rule.MetricName]
		if !ok {
			continue
		}
		triggered, value, threshold := e.checkRule(rule, snap)
		if triggered {
			e.handleTriggered(rule, value, threshold, now, snap)
		} else {
			// 未触发，重置连续计数
			if rule.RuleType == RuleTypeConsecutive {
				e.consecutive.Store(rule.MetricName, 0)
			}
		}
		// 保存上一周期值（用于突变检测）
		e.savePrevValues(rule.MetricName, snap)
	}
}

func (e *Engine) checkRule(rule Rule, snap *metrics.Snapshot) (triggered bool, value interface{}, threshold interface{}) {
	switch rule.RuleType {
	case RuleTypeImmediate:
		return e.checkImmediate(snap)
	case RuleTypeThreshold:
		return e.checkThreshold(rule, snap)
	case RuleTypeRate:
		return e.checkRate(rule, snap)
	case RuleTypeSurge:
		return e.checkSurge(rule, snap)
	case RuleTypeConsecutive:
		return e.checkConsecutive(rule, snap)
	default:
		return false, nil, nil
	}
}

// checkImmediate value > 0 即告警
func (e *Engine) checkImmediate(snap *metrics.Snapshot) (bool, interface{}, interface{}) {
	switch snap.Type {
	case metrics.MetricTypeCounter:
		if snap.Counter != nil && *snap.Counter > 0 {
			return true, *snap.Counter, int64(0)
		}
	case metrics.MetricTypeGauge:
		if snap.Gauge != nil && *snap.Gauge > 0 {
			return true, *snap.Gauge, float64(0)
		}
	}
	return false, nil, nil
}

// checkThreshold value > threshold
func (e *Engine) checkThreshold(rule Rule, snap *metrics.Snapshot) (bool, interface{}, interface{}) {
	switch snap.Type {
	case metrics.MetricTypeCounter:
		if snap.Counter != nil && float64(*snap.Counter) > rule.Threshold {
			return true, *snap.Counter, rule.Threshold
		}
	case metrics.MetricTypeGauge:
		if snap.Gauge != nil && *snap.Gauge > rule.Threshold {
			return true, *snap.Gauge, rule.Threshold
		}
	case metrics.MetricTypeHistogram:
		if snap.Histogram != nil && snap.Histogram.P99 > rule.Threshold {
			return true, snap.Histogram.P99, rule.Threshold
		}
	case metrics.MetricTypeRate:
		if snap.Rate != nil && snap.Rate.Rate > rule.Threshold {
			return true, snap.Rate.Rate, rule.Threshold
		}
	}
	return false, nil, nil
}

// checkRate fail_rate > threshold
func (e *Engine) checkRate(rule Rule, snap *metrics.Snapshot) (bool, interface{}, interface{}) {
	if snap.Type == metrics.MetricTypeRate && snap.Rate != nil {
		// 最小样本量保护：样本过少时不判定失败率，防止小样本误告警
		minSamples := rule.MinSamples
		if minSamples <= 0 {
			minSamples = defaultMinSamples
		}
		if snap.Rate.Total >= minSamples && snap.Rate.Rate > rule.Threshold {
			return true, fmt.Sprintf("%.2f%% (%d/%d)", snap.Rate.Rate*100, snap.Rate.Fail, snap.Rate.Total), fmt.Sprintf("%.2f%%", rule.Threshold*100)
		}
	}
	return false, nil, nil
}

// checkSurge 突变检测：与上一周期相比变化幅度 > threshold%
func (e *Engine) checkSurge(rule Rule, snap *metrics.Snapshot) (bool, interface{}, interface{}) {
	var currentVal float64
	switch snap.Type {
	case metrics.MetricTypeGauge:
		if snap.Gauge == nil {
			return false, nil, nil
		}
		currentVal = *snap.Gauge
	case metrics.MetricTypeCounter:
		if snap.Counter == nil {
			return false, nil, nil
		}
		currentVal = float64(*snap.Counter)
	default:
		return false, nil, nil
	}

	prevVal := 0.0
	if v, ok := e.prevGauges.Load(snap.Name); ok {
		prevVal = v.(float64)
	} else if v, ok := e.prevCounters.Load(snap.Name); ok {
		prevVal = float64(v.(int64))
	}

	// 首次无历史值，不告警
	if prevVal == 0 && currentVal == 0 {
		return false, nil, nil
	}

	var changePercent float64
	if prevVal == 0 {
		changePercent = 100.0
	} else {
		changePercent = math.Abs(currentVal-prevVal) / math.Abs(prevVal) * 100
	}

	if changePercent > rule.Threshold {
		return true, fmt.Sprintf("%.1f%% (%.0f→%.0f)", changePercent, prevVal, currentVal), fmt.Sprintf("%.0f%%", rule.Threshold)
	}
	return false, nil, nil
}

// checkConsecutive 连续 N 个周期满足条件
func (e *Engine) checkConsecutive(rule Rule, snap *metrics.Snapshot) (bool, interface{}, interface{}) {
	// 先用阈值规则判断本周期是否满足
	triggered, value, threshold := e.checkThreshold(rule, snap)
	if !triggered {
		triggered, value, threshold = e.checkImmediate(snap)
	}

	count := 0
	if v, ok := e.consecutive.Load(rule.MetricName); ok {
		count = v.(int)
	}

	if triggered {
		count++
		e.consecutive.Store(rule.MetricName, count)
		if count >= rule.ConsecutiveN {
			return true, fmt.Sprintf("%v (连续%d次)", value, count), threshold
		}
	} else {
		e.consecutive.Store(rule.MetricName, 0)
	}
	return false, nil, nil
}

func (e *Engine) handleTriggered(rule Rule, value, threshold interface{}, now time.Time, snap *metrics.Snapshot) {
	// 冷却检查
	if lastTime, ok := e.cooldowns.Load(rule.MetricName); ok {
		if now.Sub(lastTime.(time.Time)) < rule.CooldownPeriod {
			return
		}
	}

	// 检查是否启用
	if e.configProvider != nil {
		cfg, err := e.configProvider.GetNotifierConfig()
		if err == nil && cfg != nil && !cfg.Enabled {
			return
		}
	}

	event := AlertEvent{
		ServiceName: e.serviceName,
		MetricName:  rule.MetricName,
		Level:       rule.Level,
		Title:       rule.Title,
		Message:     fmt.Sprintf("服务[%s] 指标[%s] 当前值: %s, 阈值: %s", e.serviceName, rule.MetricName, formatValue(value), formatValue(threshold)),
		Value:       value,
		Threshold:   threshold,
		Tags:        snap.Tags,
		Timestamp:   now,
	}

	// 记录冷却时间
	e.cooldowns.Store(rule.MetricName, now)

	// 发送通知
	e.mu.RLock()
	notifiers := make([]Notifier, len(e.notifiers))
	copy(notifiers, e.notifiers)
	store := e.store
	e.mu.RUnlock()

	for _, n := range notifiers {
		if err := n.Send(event); err != nil {
			log.Printf("[alert] send notification failed: %v", err)
		}
	}

	// 持久化
	if store != nil {
		if err := store.Save(event); err != nil {
			log.Printf("[alert] save alert log failed: %v", err)
		}
	}
}

// formatValue 格式化告警值，float64 保留两位小数避免浮点精度问题
func formatValue(v interface{}) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%.2f", val)
	case float32:
		return fmt.Sprintf("%.2f", val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (e *Engine) savePrevValues(name string, snap *metrics.Snapshot) {
	switch snap.Type {
	case metrics.MetricTypeGauge:
		if snap.Gauge != nil {
			e.prevGauges.Store(name, *snap.Gauge)
		}
	case metrics.MetricTypeCounter:
		if snap.Counter != nil {
			e.prevCounters.Store(name, *snap.Counter)
		}
	}
}
