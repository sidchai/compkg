package metrics

import (
	"time"
)

// Config metrics 初始化配置
type Config struct {
	ServiceName       string           // 服务名称（iot_server / stream_server 等）
	AggregateInterval time.Duration    // 聚合周期，默认10s
	OnSnapshot        SnapshotCallback // 聚合回调，告警引擎在此接入
}

const defaultAggregateInterval = 10 * time.Second

var defaultRegistry *Registry

// Init 初始化全局 metrics registry，启动后台聚合协程
func Init(cfg Config) {
	if cfg.AggregateInterval <= 0 {
		cfg.AggregateInterval = defaultAggregateInterval
	}
	if defaultRegistry != nil {
		defaultRegistry.Stop()
	}
	defaultRegistry = newRegistry(cfg.ServiceName, cfg.AggregateInterval, cfg.OnSnapshot)
}

// Stop 停止全局 registry，刷出最后一批快照
func Stop() {
	if defaultRegistry != nil {
		defaultRegistry.Stop()
	}
}

// ensureRegistry 确保 defaultRegistry 已初始化（防御性：未调用 Init 时自动创建无回调的 registry）
func ensureRegistry() *Registry {
	if defaultRegistry == nil {
		defaultRegistry = newRegistry("", defaultAggregateInterval, nil)
	}
	return defaultRegistry
}

// Counter 获取/创建全局计数器
func Counter(name string) *CounterMetric {
	return ensureRegistry().GetOrCreateCounter(name)
}

// Gauge 获取/创建全局瞬时值
func Gauge(name string) *GaugeMetric {
	return ensureRegistry().GetOrCreateGauge(name)
}

// Histogram 获取/创建全局直方图
func Histogram(name string) *HistogramMetric {
	return ensureRegistry().GetOrCreateHistogram(name)
}

// Rate 获取/创建全局比率计算器
func Rate(name string) *RateMetric {
	return ensureRegistry().GetOrCreateRate(name)
}

// GetRegistry 返回全局 registry 实例（高级用法）
func GetRegistry() *Registry {
	return defaultRegistry
}
