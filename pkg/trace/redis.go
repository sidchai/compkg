package trace

import (
	"context"
	"strings"

	"github.com/go-redis/redis/v8"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// RedisHook 是 go-redis v8 的 trace hook，记录单条命令耗时。
//
// 用法：
//
//	client := redis.NewClient(...)
//	client.AddHook(trace.NewRedisHook())
type redisHook struct{}

// NewRedisHook 创建一个 go-redis v8 hook。
func NewRedisHook() redis.Hook {
	return redisHook{}
}

func (redisHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	return startRedisSpan(ctx, cmd.Name(), commandKey(cmd))
}

func (redisHook) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	endRedisSpan(ctx, cmd.Err())
	return nil
}

func (redisHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	name := "pipeline"
	if len(cmds) > 0 {
		name = cmds[0].Name() + "+" + commandKey(cmds[0])
	}
	return startRedisSpan(ctx, "pipeline", name)
}

func (redisHook) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	var firstErr error
	for _, c := range cmds {
		if err := c.Err(); err != nil {
			firstErr = err
			break
		}
	}
	endRedisSpan(ctx, firstErr)
	return nil
}

type redisSpanKey struct{}

func startRedisSpan(ctx context.Context, command, keyPattern string) (context.Context, error) {
	if !Enabled() {
		return ctx, nil
	}
	tr := Tracer("compkg/trace/redis")
	name := "redis." + strings.ToLower(command)
	if keyPattern != "" {
		name = name + " " + keyPattern
	}
	c, span := tr.Start(ctx, name,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			semconv.DBSystemRedis,
			attribute.String("db.statement", command),
		),
	)
	return context.WithValue(c, redisSpanKey{}, span), nil
}

func endRedisSpan(ctx context.Context, err error) {
	span, ok := ctx.Value(redisSpanKey{}).(oteltrace.Span)
	if !ok {
		return
	}
	if err != nil && err != redis.Nil {
		span.RecordError(err)
	}
	span.End()
}

// commandKey 取命令第一个 key 作为 span 命名，方便定位热点 key 模板。
// 不入库 value，避免 PII。
func commandKey(cmd redis.Cmder) string {
	args := cmd.Args()
	if len(args) < 2 {
		return ""
	}
	if k, ok := args[1].(string); ok {
		return collapseKey(k)
	}
	return ""
}

// collapseKey 把 key 中的数字段折叠成 {n}，避免 span name 维度爆炸。
//
//	stream:session:abc123  → stream:session:{n}
//	iot:instruction:node5  → iot:instruction:{n}
func collapseKey(k string) string {
	parts := strings.Split(k, ":")
	for i, p := range parts {
		if hasDigit(p) {
			parts[i] = "{n}"
		}
	}
	return strings.Join(parts, ":")
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
