package metrics

import (
	"math"
	"sync/atomic"
)

// GaugeMetric 瞬时值指标（连接数、队列长度、在线设备数等）
type GaugeMetric struct {
	name string
	bits uint64
	tags map[string]string
}

func newGauge(name string) *GaugeMetric {
	return &GaugeMetric{
		name: name,
		tags: make(map[string]string),
	}
}

// Set 设置瞬时值
func (g *GaugeMetric) Set(v float64) {
	atomic.StoreUint64(&g.bits, math.Float64bits(v))
}

// Inc +1
func (g *GaugeMetric) Inc() {
	for {
		old := atomic.LoadUint64(&g.bits)
		newVal := math.Float64frombits(old) + 1
		if atomic.CompareAndSwapUint64(&g.bits, old, math.Float64bits(newVal)) {
			return
		}
	}
}

// Dec -1
func (g *GaugeMetric) Dec() {
	for {
		old := atomic.LoadUint64(&g.bits)
		newVal := math.Float64frombits(old) - 1
		if atomic.CompareAndSwapUint64(&g.bits, old, math.Float64bits(newVal)) {
			return
		}
	}
}

// Add 增加指定值
func (g *GaugeMetric) Add(delta float64) {
	for {
		old := atomic.LoadUint64(&g.bits)
		newVal := math.Float64frombits(old) + delta
		if atomic.CompareAndSwapUint64(&g.bits, old, math.Float64bits(newVal)) {
			return
		}
	}
}

// Value 读取当前值
func (g *GaugeMetric) Value() float64 {
	return math.Float64frombits(atomic.LoadUint64(&g.bits))
}

// WithTags 设置标签
func (g *GaugeMetric) WithTags(tags map[string]string) *GaugeMetric {
	for k, v := range tags {
		g.tags[k] = v
	}
	return g
}

// Name 返回指标名
func (g *GaugeMetric) Name() string {
	return g.name
}

// Tags 返回标签
func (g *GaugeMetric) Tags() map[string]string {
	return g.tags
}
