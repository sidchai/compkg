package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusExporter 将自定义 metrics 桥接到 prometheus registry
// 在聚合回调中被调用，将快照数据同步到 prometheus
type PrometheusExporter struct {
	registry *prometheus.Registry

	// prometheus 原生指标缓存（避免重复创建）
	promCounters   sync.Map // name -> prometheus.Counter
	promGauges     sync.Map // name -> prometheus.Gauge
	promHistograms sync.Map // name -> prometheus.Summary (用 Summary 表达分位数)
	promRates      sync.Map // name -> prometheus.Gauge (失败率用 gauge 表示)
}

// NewPrometheusExporter 创建 exporter，传入自定义 registry 或 nil（使用默认 registry）
func NewPrometheusExporter(reg *prometheus.Registry) *PrometheusExporter {
	if reg == nil {
		reg = prometheus.DefaultRegisterer.(*prometheus.Registry)
	}
	return &PrometheusExporter{
		registry: reg,
	}
}

// ExportSnapshots 导出快照到 prometheus（在聚合回调中调用）
func (e *PrometheusExporter) ExportSnapshots(snapshots map[string]*Snapshot) {
	for _, snap := range snapshots {
		switch snap.Type {
		case MetricTypeCounter:
			e.exportCounter(snap)
		case MetricTypeGauge:
			e.exportGauge(snap)
		case MetricTypeHistogram:
			e.exportHistogram(snap)
		case MetricTypeRate:
			e.exportRate(snap)
		}
	}
}

// exportCounter 导出计数器（prometheus Counter 只增不减，用 Add 累加）
func (e *PrometheusExporter) exportCounter(snap *Snapshot) {
	if snap.Counter == nil {
		return
	}

	key := snap.Name
	var counter prometheus.Counter

	if v, ok := e.promCounters.Load(key); ok {
		counter = v.(prometheus.Counter)
	} else {
		counter = prometheus.NewCounter(prometheus.CounterOpts{
			Name:        sanitizeName(snap.Name),
			Help:        snap.Name,
			ConstLabels: snap.Tags,
		})
		if err := e.registry.Register(counter); err != nil {
			// 已注册则使用已有的，避免 panic
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				counter = are.ExistingCollector.(prometheus.Counter)
			} else {
				return
			}
		}
		e.promCounters.Store(key, counter)
	}

	// 自定义 Counter.Reset() 返回的是增量，直接 Add
	if *snap.Counter > 0 {
		counter.Add(float64(*snap.Counter))
	}
}

// exportGauge 导出瞬时值
func (e *PrometheusExporter) exportGauge(snap *Snapshot) {
	if snap.Gauge == nil {
		return
	}

	key := snap.Name
	var gauge prometheus.Gauge

	if v, ok := e.promGauges.Load(key); ok {
		gauge = v.(prometheus.Gauge)
	} else {
		gauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        sanitizeName(snap.Name),
			Help:        snap.Name,
			ConstLabels: snap.Tags,
		})
		if err := e.registry.Register(gauge); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				gauge = are.ExistingCollector.(prometheus.Gauge)
			} else {
				return
			}
		}
		e.promGauges.Store(key, gauge)
	}

	gauge.Set(*snap.Gauge)
}

// exportHistogram 导出分布指标（用 Summary 表达 P50/P95/P99）
// 注意：prometheus Summary 需要在 Observe 时计算分位数，这里用 Gauge 导出各分位数值
func (e *PrometheusExporter) exportHistogram(snap *Snapshot) {
	if snap.Histogram == nil || snap.Histogram.Count == 0 {
		return
	}

	h := snap.Histogram
	baseName := sanitizeName(snap.Name)

	// 为每个分位数创建独立的 Gauge
	e.exportHistogramQuantile(baseName+"_p50", snap.Tags, h.P50)
	e.exportHistogramQuantile(baseName+"_p95", snap.Tags, h.P95)
	e.exportHistogramQuantile(baseName+"_p99", snap.Tags, h.P99)
	e.exportHistogramQuantile(baseName+"_avg", snap.Tags, h.Avg)
	e.exportHistogramQuantile(baseName+"_max", snap.Tags, h.Max)
	e.exportHistogramQuantile(baseName+"_min", snap.Tags, h.Min)
	e.exportHistogramQuantile(baseName+"_count", snap.Tags, float64(h.Count))
}

func (e *PrometheusExporter) exportHistogramQuantile(name string, tags map[string]string, value float64) {
	var gauge prometheus.Gauge

	if v, ok := e.promGauges.Load(name); ok {
		gauge = v.(prometheus.Gauge)
	} else {
		gauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        name,
			Help:        name,
			ConstLabels: tags,
		})
		if err := e.registry.Register(gauge); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				gauge = are.ExistingCollector.(prometheus.Gauge)
			} else {
				return
			}
		}
		e.promGauges.Store(name, gauge)
	}

	gauge.Set(value)
}

// exportRate 导出比率指标（用多个 Gauge 表示 total/success/fail/rate）
func (e *PrometheusExporter) exportRate(snap *Snapshot) {
	if snap.Rate == nil {
		return
	}

	r := snap.Rate
	baseName := sanitizeName(snap.Name)

	e.exportRateGauge(baseName+"_total", snap.Tags, float64(r.Total))
	e.exportRateGauge(baseName+"_success", snap.Tags, float64(r.Success))
	e.exportRateGauge(baseName+"_fail", snap.Tags, float64(r.Fail))
	e.exportRateGauge(baseName+"_rate", snap.Tags, r.Rate)
}

func (e *PrometheusExporter) exportRateGauge(name string, tags map[string]string, value float64) {
	var gauge prometheus.Gauge

	if v, ok := e.promRates.Load(name); ok {
		gauge = v.(prometheus.Gauge)
	} else {
		gauge = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        name,
			Help:        name,
			ConstLabels: tags,
		})
		if err := e.registry.Register(gauge); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				gauge = are.ExistingCollector.(prometheus.Gauge)
			} else {
				return
			}
		}
		e.promRates.Store(name, gauge)
	}

	gauge.Set(value)
}

// sanitizeName 清理指标名，prometheus 要求 [a-zA-Z_:][a-zA-Z0-9_:]*
func sanitizeName(name string) string {
	// 简单实现：将非法字符替换为下划线
	runes := []rune(name)
	for i, r := range runes {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ':') {
			runes[i] = '_'
		}
	}
	// 首字符不能是数字
	if len(runes) > 0 && runes[0] >= '0' && runes[0] <= '9' {
		runes = append([]rune{'_'}, runes...)
	}
	return string(runes)
}
