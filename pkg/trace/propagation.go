package trace

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// W3C 传播 Header 常量；业务侧（HTTP/gRPC/MQ/Pub-Sub）必须使用这一组键名。
const (
	HeaderTraceparent = "traceparent"
	HeaderTracestate  = "tracestate"
	HeaderBaggage     = "baggage"
)

// PubSubTraceField 是 Redis Pub/Sub / MQ payload 中携带 trace 元数据的字段名（RFC §5.2）。
const PubSubTraceField = "_trace"

// TraceContextEnvelope 用于 Redis Pub/Sub / MQ payload 的 _trace 子字段。
//
// 业务 payload 不变，只在最外层新增：
//
//	{
//	  "...": "...",
//	  "_trace": {"traceparent": "00-...-...-01", "tracestate": ""}
//	}
type TraceContextEnvelope struct {
	Traceparent string `json:"traceparent,omitempty"`
	Tracestate  string `json:"tracestate,omitempty"`
}

// MapCarrier 是 propagation.MapCarrier 的别名，方便业务直接构建。
type MapCarrier = propagation.MapCarrier

// Inject 把当前 span context 注入 header map（HTTP/gRPC metadata/MQ header）。
//
// carrier 既可以是 propagation.MapCarrier，也可以是其他实现 TextMapCarrier 的对象。
func Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// Extract 从 header map 提取 span context 写入返回的 ctx；
// 后续在该 ctx 下 StartSpan 会自动续接 parent。
func Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectMap 是 Inject 的便捷封装，返回新的 map[string]string，
// 用于 Redis Pub/Sub / MQ payload 这种没有 carrier 的场景。
func InjectMap(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

// InjectEnvelope 返回 TraceContextEnvelope，便于业务塞进 JSON payload 的 _trace 字段。
func InjectEnvelope(ctx context.Context) TraceContextEnvelope {
	m := InjectMap(ctx)
	return TraceContextEnvelope{
		Traceparent: m[HeaderTraceparent],
		Tracestate:  m[HeaderTracestate],
	}
}

// ExtractEnvelope 从 TraceContextEnvelope 恢复 ctx；envelope 为零值时返回原 ctx。
func ExtractEnvelope(ctx context.Context, env TraceContextEnvelope) context.Context {
	if env.Traceparent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{
		HeaderTraceparent: env.Traceparent,
	}
	if env.Tracestate != "" {
		carrier[HeaderTracestate] = env.Tracestate
	}
	return Extract(ctx, carrier)
}

// SpanContextFromContext 取出当前 ctx 的 SpanContext；未启用时返回 invalid。
func SpanContextFromContext(ctx context.Context) oteltrace.SpanContext {
	return oteltrace.SpanContextFromContext(ctx)
}

// TraceIDFromContext 返回 32-hex traceId 或空串。
func TraceIDFromContext(ctx context.Context) string {
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext 返回 16-hex spanId 或空串。
func SpanIDFromContext(ctx context.Context) string {
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}
