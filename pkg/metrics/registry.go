package metrics

import (
	"sync"
	"time"
)

// SnapshotCallback 聚合快照回调函数类型
type SnapshotCallback func(snapshots map[string]*Snapshot)

// Registry 全局指标注册表
type Registry struct {
	counters   sync.Map // name -> *CounterMetric
	gauges     sync.Map // name -> *GaugeMetric
	histograms sync.Map // name -> *HistogramMetric
	rates      sync.Map // name -> *RateMetric

	serviceName string
	interval    time.Duration
	onSnapshot  SnapshotCallback
	stopCh      chan struct{}
	stopped     chan struct{}
}

func newRegistry(serviceName string, interval time.Duration, onSnapshot SnapshotCallback) *Registry {
	r := &Registry{
		serviceName: serviceName,
		interval:    interval,
		onSnapshot:  onSnapshot,
		stopCh:      make(chan struct{}),
		stopped:     make(chan struct{}),
	}
	go r.aggregateLoop()
	return r
}

// Stop 停止聚合协程，刷出最后一批快照
func (r *Registry) Stop() {
	close(r.stopCh)
	<-r.stopped
}

// GetOrCreateCounter 获取或创建计数器
func (r *Registry) GetOrCreateCounter(name string) *CounterMetric {
	if v, ok := r.counters.Load(name); ok {
		return v.(*CounterMetric)
	}
	c := newCounter(name)
	actual, _ := r.counters.LoadOrStore(name, c)
	return actual.(*CounterMetric)
}

// GetOrCreateGauge 获取或创建瞬时值
func (r *Registry) GetOrCreateGauge(name string) *GaugeMetric {
	if v, ok := r.gauges.Load(name); ok {
		return v.(*GaugeMetric)
	}
	g := newGauge(name)
	actual, _ := r.gauges.LoadOrStore(name, g)
	return actual.(*GaugeMetric)
}

// GetOrCreateHistogram 获取或创建直方图
func (r *Registry) GetOrCreateHistogram(name string) *HistogramMetric {
	if v, ok := r.histograms.Load(name); ok {
		return v.(*HistogramMetric)
	}
	h := newHistogram(name)
	actual, _ := r.histograms.LoadOrStore(name, h)
	return actual.(*HistogramMetric)
}

// GetOrCreateRate 获取或创建比率计算器
func (r *Registry) GetOrCreateRate(name string) *RateMetric {
	if v, ok := r.rates.Load(name); ok {
		return v.(*RateMetric)
	}
	rt := newRate(name)
	actual, _ := r.rates.LoadOrStore(name, rt)
	return actual.(*RateMetric)
}

func (r *Registry) aggregateLoop() {
	defer close(r.stopped)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.collectAndNotify()
		case <-r.stopCh:
			// 最后刷一次
			r.collectAndNotify()
			return
		}
	}
}

func (r *Registry) collectAndNotify() {
	if r.onSnapshot == nil {
		return
	}
	now := time.Now()
	snapshots := make(map[string]*Snapshot)

	r.counters.Range(func(key, value interface{}) bool {
		name := key.(string)
		c := value.(*CounterMetric)
		v := c.Reset()
		snapshots[name] = &Snapshot{
			Name:        name,
			Type:        MetricTypeCounter,
			Counter:     &v,
			Tags:        c.Tags(),
			ServiceName: r.serviceName,
			Timestamp:   now,
		}
		return true
	})

	r.gauges.Range(func(key, value interface{}) bool {
		name := key.(string)
		g := value.(*GaugeMetric)
		v := g.Value()
		snapshots[name] = &Snapshot{
			Name:        name,
			Type:        MetricTypeGauge,
			Gauge:       &v,
			Tags:        g.Tags(),
			ServiceName: r.serviceName,
			Timestamp:   now,
		}
		return true
	})

	r.histograms.Range(func(key, value interface{}) bool {
		name := key.(string)
		h := value.(*HistogramMetric)
		snap := h.Reset()
		snapshots[name] = &Snapshot{
			Name:        name,
			Type:        MetricTypeHistogram,
			Histogram:   &snap,
			Tags:        h.Tags(),
			ServiceName: r.serviceName,
			Timestamp:   now,
		}
		return true
	})

	r.rates.Range(func(key, value interface{}) bool {
		name := key.(string)
		rt := value.(*RateMetric)
		snap := rt.Reset()
		snapshots[name] = &Snapshot{
			Name:        name,
			Type:        MetricTypeRate,
			Rate:        &snap,
			Tags:        rt.Tags(),
			ServiceName: r.serviceName,
			Timestamp:   now,
		}
		return true
	})

	if len(snapshots) > 0 {
		r.onSnapshot(snapshots)
	}
}
