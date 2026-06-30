package trace

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
)

// MQ Header inject/extract 工具。
//
// 不直接依赖某个 MQ SDK（Kafka / RocketMQ / NSQ 各自 header 类型不同），
// 提供 map[string]string 适配层；业务侧把 header 序列化到自家 SDK 即可。
//
// Producer 用法：
//
//	headers := trace.MQInject(ctx)
//	for k, v := range headers { msg.SetProperty(k, v) }
//
// Consumer 用法：
//
//	headers := map[string]string{}
//	for _, p := range msg.Properties { headers[p.Key] = p.Value }
//	ctx = trace.MQExtract(ctx, headers)
//	ctx, span := tracer.Start(ctx, "mq.consume topic")
//	defer span.End()

// MQInject 从 ctx 提取 traceparent 并返回 map（含 traceparent / tracestate）。
func MQInject(ctx context.Context) map[string]string {
	return InjectMap(ctx)
}

// MQExtract 从 header map 还原 ctx；headers 为空时返回原 ctx。
func MQExtract(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	carrier := propagation.MapCarrier(headers)
	return Extract(ctx, carrier)
}
