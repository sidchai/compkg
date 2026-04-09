package metrics

import (
	"sync/atomic"
)

// RateMetric 比率计算器（失败率、超时率等）
type RateMetric struct {
	name    string
	success int64
	fail    int64
	tags    map[string]string
}

// RateSnapshot 比率快照
type RateSnapshot struct {
	Total   int64   `json:"total"`
	Success int64   `json:"success"`
	Fail    int64   `json:"fail"`
	Rate    float64 `json:"rate"` // fail / total
}

func newRate(name string) *RateMetric {
	return &RateMetric{
		name: name,
		tags: make(map[string]string),
	}
}

// RecordSuccess 记录一次成功
func (r *RateMetric) RecordSuccess() {
	atomic.AddInt64(&r.success, 1)
}

// RecordFail 记录一次失败
func (r *RateMetric) RecordFail() {
	atomic.AddInt64(&r.fail, 1)
}

// Snapshot 获取当前快照（不清空）
func (r *RateMetric) Snapshot() RateSnapshot {
	s := atomic.LoadInt64(&r.success)
	f := atomic.LoadInt64(&r.fail)
	return buildRateSnapshot(s, f)
}

// Reset 获取快照并归零（聚合周期结束时调用）
func (r *RateMetric) Reset() RateSnapshot {
	s := atomic.SwapInt64(&r.success, 0)
	f := atomic.SwapInt64(&r.fail, 0)
	return buildRateSnapshot(s, f)
}

// WithTags 设置标签
func (r *RateMetric) WithTags(tags map[string]string) *RateMetric {
	for k, v := range tags {
		r.tags[k] = v
	}
	return r
}

// Name 返回指标名
func (r *RateMetric) Name() string {
	return r.name
}

// Tags 返回标签
func (r *RateMetric) Tags() map[string]string {
	return r.tags
}

func buildRateSnapshot(success, fail int64) RateSnapshot {
	total := success + fail
	rate := 0.0
	if total > 0 {
		rate = float64(fail) / float64(total)
	}
	return RateSnapshot{
		Total:   total,
		Success: success,
		Fail:    fail,
		Rate:    rate,
	}
}
