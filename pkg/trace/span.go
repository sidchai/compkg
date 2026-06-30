package trace

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// 业务侧手动开 span 的便捷封装。命名规范见 RFC §10.4。
//
// 用法：
//
//	ctx, span := trace.StartSpan(ctx, "waiter.wait")
//	defer span.End()
//	// ... 业务逻辑
//	if err != nil { span.RecordError(err) }

// StartSpan 在当前 ctx 下创建 internal span。
func StartSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	return Tracer("compkg/trace").Start(ctx, name, opts...)
}

// StartConsumerSpan 用于 MQ / Redis Pub/Sub 消费侧；自动设置 SpanKind=Consumer。
func StartConsumerSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	opts = append(opts, oteltrace.WithSpanKind(oteltrace.SpanKindConsumer))
	return Tracer("compkg/trace").Start(ctx, name, opts...)
}

// StartProducerSpan 用于 MQ / Redis Pub/Sub 生产侧；自动设置 SpanKind=Producer。
func StartProducerSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	opts = append(opts, oteltrace.WithSpanKind(oteltrace.SpanKindProducer))
	return Tracer("compkg/trace").Start(ctx, name, opts...)
}

// RecordError 标记 span 错误并记录，nil 时无操作。
func RecordError(span oteltrace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
}

// SpanFromContext 与 oteltrace.SpanFromContext 等价；为业务侧避免再 import oteltrace 准备。
func SpanFromContext(ctx context.Context) oteltrace.Span {
	return oteltrace.SpanFromContext(ctx)
}
