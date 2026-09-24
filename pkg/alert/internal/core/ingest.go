package core

// Sample 热路径通用采样。禁止带 device_no 等作为 Prometheus 标签。
type Sample struct {
	Kind    string
	Key     string
	Numeric float64
	Flag    bool
	Extra   map[string]string
	AtUnix  int64
}

// SampleIngester 热路径非阻塞摄入。满则丢 + 自检计数。
type SampleIngester interface {
	Ingest(sample Sample)
}
