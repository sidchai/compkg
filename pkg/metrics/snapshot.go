package metrics

import (
	"time"
)

// MetricType 指标类型
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeRate      MetricType = "rate"
)

// Snapshot 聚合快照，一个指标一个快照
type Snapshot struct {
	Name        string             `json:"name"`
	Type        MetricType         `json:"type"`
	Counter     *int64             `json:"counter,omitempty"`
	Gauge       *float64           `json:"gauge,omitempty"`
	Histogram   *HistogramSnapshot `json:"histogram,omitempty"`
	Rate        *RateSnapshot      `json:"rate,omitempty"`
	Tags        map[string]string  `json:"tags,omitempty"`
	ServiceName string             `json:"serviceName"`
	Timestamp   time.Time          `json:"timestamp"`
}
