package metrics

import (
	"math"
	"sort"
	"sync"
)

// HistogramMetric 延迟分布指标（P50/P95/P99/Avg/Max）
type HistogramMetric struct {
	name    string
	values  []float64
	mu      sync.Mutex
	tags    map[string]string
	maxSize int // 单周期最大样本数，防止内存膨胀
}

// HistogramSnapshot 直方图快照
type HistogramSnapshot struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Avg   float64 `json:"avg"`
	Max   float64 `json:"max"`
	Min   float64 `json:"min"`
}

const defaultMaxSampleSize = 10000

func newHistogram(name string) *HistogramMetric {
	return &HistogramMetric{
		name:    name,
		values:  make([]float64, 0, 256),
		tags:    make(map[string]string),
		maxSize: defaultMaxSampleSize,
	}
}

// Observe 记录一次观测值（如耗时 ms）
func (h *HistogramMetric) Observe(value float64) {
	h.mu.Lock()
	if len(h.values) < h.maxSize {
		h.values = append(h.values, value)
	}
	h.mu.Unlock()
}

// Snapshot 获取当前快照（不清空数据）
func (h *HistogramMetric) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	copied := make([]float64, len(h.values))
	copy(copied, h.values)
	h.mu.Unlock()
	return calcSnapshot(copied)
}

// Reset 获取快照并清空数据（聚合周期结束时调用）
func (h *HistogramMetric) Reset() HistogramSnapshot {
	h.mu.Lock()
	copied := h.values
	h.values = make([]float64, 0, 256)
	h.mu.Unlock()
	return calcSnapshot(copied)
}

// WithTags 设置标签
func (h *HistogramMetric) WithTags(tags map[string]string) *HistogramMetric {
	h.mu.Lock()
	for k, v := range tags {
		h.tags[k] = v
	}
	h.mu.Unlock()
	return h
}

// Name 返回指标名
func (h *HistogramMetric) Name() string {
	return h.name
}

// Tags 返回标签
func (h *HistogramMetric) Tags() map[string]string {
	return h.tags
}

func calcSnapshot(values []float64) HistogramSnapshot {
	n := len(values)
	if n == 0 {
		return HistogramSnapshot{}
	}

	sort.Float64s(values)

	sum := 0.0
	maxVal := math.SmallestNonzeroFloat64
	minVal := math.MaxFloat64
	for _, v := range values {
		sum += v
		if v > maxVal {
			maxVal = v
		}
		if v < minVal {
			minVal = v
		}
	}

	return HistogramSnapshot{
		Count: n,
		P50:   percentile(values, 0.50),
		P95:   percentile(values, 0.95),
		P99:   percentile(values, 0.99),
		Avg:   sum / float64(n),
		Max:   maxVal,
		Min:   minVal,
	}
}

func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := p * float64(n-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
