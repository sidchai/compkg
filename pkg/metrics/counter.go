package metrics

import (
	"sync/atomic"
)

// CounterMetric 原子计数器（累计型：失败次数、断开次数等）
type CounterMetric struct {
	name  string
	value int64
	tags  map[string]string
}

func newCounter(name string) *CounterMetric {
	return &CounterMetric{
		name: name,
		tags: make(map[string]string),
	}
}

// Inc 计数+1
func (c *CounterMetric) Inc() {
	atomic.AddInt64(&c.value, 1)
}

// Add 计数+N
func (c *CounterMetric) Add(delta int64) {
	atomic.AddInt64(&c.value, delta)
}

// Value 读取当前值
func (c *CounterMetric) Value() int64 {
	return atomic.LoadInt64(&c.value)
}

// Reset 读取并归零（聚合周期结束时调用）
func (c *CounterMetric) Reset() int64 {
	return atomic.SwapInt64(&c.value, 0)
}

// WithTags 设置标签（返回自身，支持链式调用）
func (c *CounterMetric) WithTags(tags map[string]string) *CounterMetric {
	for k, v := range tags {
		c.tags[k] = v
	}
	return c
}

// Name 返回指标名
func (c *CounterMetric) Name() string {
	return c.name
}

// Tags 返回标签
func (c *CounterMetric) Tags() map[string]string {
	return c.tags
}
