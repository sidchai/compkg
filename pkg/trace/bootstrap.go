// Package trace 是 RFC-07 全链路追踪 SDK，基于 OpenTelemetry。
//
// 设计原则：
//   - 唯一传播协议：W3C Trace Context（traceparent / tracestate）
//   - 入口/出口中间件优先；业务侧只补关键 attribute
//   - 采样在 Collector tail sampling 决策；SDK 默认 AlwaysSample 全量上报
//   - SDK 不可阻塞业务启动：开关关闭、Collector 不可达时降级为 no-op
//   - 与 compkg/pkg/logger 联动：Bootstrap 内部注册 logger.SetTraceExtractor
//
// 启动顺序见 RFC §8.2：
//  1. 初始化配置中心
//  2. logger.Bootstrap
//  3. trace.Bootstrap
//  4. 业务入口启动
package trace

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// BootstrapOptions 控制 Bootstrap 行为，所有字段均可选。
//
// 字段语义：
//   - ServiceName：service.name resource，必填，用于 Jaeger 服务过滤
//   - ServiceVersion：service.version，可选
//   - Environment：deployment.environment，建议传 cloud/private/dev/test/prod
//   - CollectorEndpoint：OTLP gRPC 地址（host:port），不带 scheme
//   - Enabled：false 时直接走 no-op，业务零成本
//   - Insecure：dev/内网设为 true 跳过 TLS
//   - ExportTimeout：单次导出超时
//   - BatchTimeout：BatchSpanProcessor 触发上报的最长等待
//   - MaxQueueSize：进程内 span 队列上限，溢出后丢弃并计数
//   - MaxExportBatchSize：单批最大 span 数
//   - AttributePolicy：业务 attribute 白名单/脱敏策略；nil 时使用 DefaultAttributePolicy
//   - ExtraResourceAttrs：附加 resource 标签（host.name 等）
type BootstrapOptions struct {
	ServiceName        string
	ServiceVersion     string
	Environment        string
	CollectorEndpoint  string
	Enabled            bool
	Insecure           bool
	ExportTimeout      time.Duration
	BatchTimeout       time.Duration
	MaxQueueSize       int
	MaxExportBatchSize int
	AttributePolicy    *AttributePolicy
	ExtraResourceAttrs []attribute.KeyValue
}

const (
	defaultExportTimeout      = 3 * time.Second
	defaultBatchTimeout       = 5 * time.Second
	defaultMaxQueueSize       = 2048
	defaultMaxExportBatchSize = 512
)

// 全局开关 + Tracer Provider。Bootstrap 之后会被原子设置。
var (
	enabled        atomic.Bool
	tracerProvider atomic.Pointer[sdktrace.TracerProvider]
	currentPolicy  atomic.Pointer[AttributePolicy]
	bootstrapOnce  atomic.Bool
)

// ShutdownFunc 返回的 shutdown 函数；调用时 flush 队列并关闭 exporter。
type ShutdownFunc func(ctx context.Context) error

// Bootstrap 初始化 OTel TracerProvider 与全局 propagator。
//
// 失败处理：
//   - opts.Enabled=false：返回 no-op shutdown，TracerProvider 为 noopTracerProvider
//   - exporter 初始化失败：返回错误，业务自行决定是否降级（建议 log + 继续启动）
//
// 多次调用：仅第一次生效，后续调用直接返回 no-op shutdown 与 ErrAlreadyBootstrapped。
func Bootstrap(ctx context.Context, opts BootstrapOptions) (ShutdownFunc, error) {
	if !bootstrapOnce.CompareAndSwap(false, true) {
		return func(context.Context) error { return nil }, ErrAlreadyBootstrapped
	}

	// W3C 传播协议必须始终注册（包括 disabled 场景）：
	// 因为 disabled 模式下，仍可能从上游收到 traceparent 并写入 logger，
	// 业务应能在跨服务时透传 traceparent。
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	policy := opts.AttributePolicy
	if policy == nil {
		policy = DefaultAttributePolicy()
	}
	currentPolicy.Store(policy)

	// 注册 logger 桥接（无论 enabled 如何都需要：disabled 时 SpanContextFromContext 返回空，
	// logger 字段为空，与 logger 包默认行为一致）。
	registerLoggerBridge()

	if !opts.Enabled {
		enabled.Store(false)
		otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	if opts.ServiceName == "" {
		return func(context.Context) error { return nil }, errors.New("trace: ServiceName is required")
	}
	if opts.CollectorEndpoint == "" {
		return func(context.Context) error { return nil }, errors.New("trace: CollectorEndpoint is required")
	}

	res, err := buildResource(ctx, opts)
	if err != nil {
		return func(context.Context) error { return nil }, fmt.Errorf("trace: build resource: %w", err)
	}

	exporter, err := buildExporter(ctx, opts)
	if err != nil {
		return func(context.Context) error { return nil }, fmt.Errorf("trace: build exporter: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithBatchTimeout(orDefaultDur(opts.BatchTimeout, defaultBatchTimeout)),
		sdktrace.WithExportTimeout(orDefaultDur(opts.ExportTimeout, defaultExportTimeout)),
		sdktrace.WithMaxQueueSize(orDefaultInt(opts.MaxQueueSize, defaultMaxQueueSize)),
		sdktrace.WithMaxExportBatchSize(orDefaultInt(opts.MaxExportBatchSize, defaultMaxExportBatchSize)),
	)

	tp := sdktrace.NewTracerProvider(
		// 全量采样 → 由 Collector tail sampling 决策（RFC §5.4）
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tp)
	tracerProvider.Store(tp)
	enabled.Store(true)

	// shutdown 顺序：TracerProvider.Shutdown 会 flush BSP 并 close exporter。
	return func(c context.Context) error {
		enabled.Store(false)
		return tp.Shutdown(c)
	}, nil
}

// Enabled 当前 SDK 是否启用（启动失败、未 Bootstrap、shutdown 之后均为 false）。
func Enabled() bool { return enabled.Load() }

// ErrAlreadyBootstrapped 表示重复调用 Bootstrap。
var ErrAlreadyBootstrapped = errors.New("trace: already bootstrapped")

// Tracer 返回命名 Tracer；未 Bootstrap 时返回 no-op。
func Tracer(name string) oteltrace.Tracer {
	return otel.Tracer(name)
}

func buildResource(ctx context.Context, opts BootstrapOptions) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(opts.ServiceName),
	}
	if opts.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(opts.ServiceVersion))
	}
	if opts.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironment(opts.Environment))
	}
	attrs = append(attrs, opts.ExtraResourceAttrs...)

	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
}

func buildExporter(ctx context.Context, opts BootstrapOptions) (*otlptrace.Exporter, error) {
	clientOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(opts.CollectorEndpoint),
		otlptracegrpc.WithCompressor("gzip"),
	}
	if opts.Insecure {
		clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
	}
	timeout := orDefaultDur(opts.ExportTimeout, defaultExportTimeout)
	clientOpts = append(clientOpts, otlptracegrpc.WithTimeout(timeout))

	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return otlptracegrpc.New(c, clientOpts...)
}

func orDefaultDur(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

func orDefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
