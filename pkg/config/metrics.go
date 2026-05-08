package config

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// 本文件实现 RFC-06 §指标 要求的 6 个 Prometheus 指标：
//
//	config_fetch_total{service, source, result}    # 拉取次数（result=ok|error）
//	config_fetch_duration_ms{service, source}      # 拉取耗时
//	config_listen_total{service, data_id}          # 远端变更订阅触发次数
//	config_validation_failure_total{service}       # Schema 校验失败次数
//	config_nacos_disconnect_total                  # Nacos 断连次数
//	config_local_fallback_used_total{service}      # 使用本地 fallback 启动次数
//
// 指标通过 sync.Once 注册到默认 Prometheus Registry，多次 Bootstrap 不会重复注册。
// 业务进程调用方无需额外配置，只要暴露 /metrics 即可采集。

var (
	metricsOnce sync.Once

	fetchTotal        *prometheus.CounterVec
	fetchDurationMs   *prometheus.HistogramVec
	listenTotal       *prometheus.CounterVec
	validationFailure *prometheus.CounterVec
	nacosDisconnect   prometheus.Counter
	localFallbackUsed *prometheus.CounterVec
)

// initMetrics 懒初始化；可重复调用，只会注册一次。
//
// 特意使用 MustRegister + Once 包裹：若外部已自行注册同名指标，MustRegister 会 panic，
// sync.Once 避免第二次 Bootstrap 时再次触发 panic。
func initMetrics() {
	metricsOnce.Do(func() {
		fetchTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "config_fetch_total",
				Help: "Total number of remote config fetches by result.",
			},
			[]string{"service", "source", "result"},
		)
		fetchDurationMs = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "config_fetch_duration_ms",
				Help:    "Duration in milliseconds of remote config fetches.",
				Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			},
			[]string{"service", "source"},
		)
		listenTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "config_listen_total",
				Help: "Total number of remote config change events applied.",
			},
			[]string{"service", "data_id"},
		)
		validationFailure = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "config_validation_failure_total",
				Help: "Total number of schema validation failures (bootstrap + hot reload).",
			},
			[]string{"service"},
		)
		nacosDisconnect = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "config_nacos_disconnect_total",
				Help: "Total number of detected Nacos disconnects (fetch error).",
			},
		)
		localFallbackUsed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "config_local_fallback_used_total",
				Help: "Total number of bootstrap events that had to fall back to local config.",
			},
			[]string{"service"},
		)

		// 注册到默认 Registry。若外部已注册同名指标，这里 panic 语义明确暴露冲突。
		prometheus.MustRegister(
			fetchTotal,
			fetchDurationMs,
			listenTotal,
			validationFailure,
			nacosDisconnect,
			localFallbackUsed,
		)
	})
}

// ---------- helper：打点前兜底初始化 ----------

// labelOrEmpty 空 service 用 "unknown" 兜底，避免 Prometheus label 出现空值警告。
func labelOrEmpty(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func metricsFetchTotalInc(service, source, result string) {
	initMetrics()
	fetchTotal.WithLabelValues(labelOrEmpty(service), labelOrEmpty(source), result).Inc()
}

func metricsFetchDurationObserve(service, source string, ms float64) {
	initMetrics()
	fetchDurationMs.WithLabelValues(labelOrEmpty(service), labelOrEmpty(source)).Observe(ms)
}

func metricsListenTotalInc(service, dataId string) {
	initMetrics()
	listenTotal.WithLabelValues(labelOrEmpty(service), labelOrEmpty(dataId)).Inc()
}

func metricsValidationFailureInc(service string) {
	initMetrics()
	validationFailure.WithLabelValues(labelOrEmpty(service)).Inc()
}

func metricsNacosDisconnectInc() {
	initMetrics()
	nacosDisconnect.Inc()
}

func metricsLocalFallbackUsedInc(service string) {
	initMetrics()
	localFallbackUsed.WithLabelValues(labelOrEmpty(service)).Inc()
}

// MetricsGatherer 方便业务侧在集成测试或自定义 /metrics handler 时取得已注册的 collector。
//
// 正常情况业务无需调用——直接暴露 promhttp.Handler() 即可，因为指标都挂在 default Registry。
func MetricsGatherer() prometheus.Gatherer {
	initMetrics()
	return prometheus.DefaultGatherer
}
