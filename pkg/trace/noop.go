package trace

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// NoopSpan 在 Enabled()==false 时使用，返回的 ctx/span 不影响业务执行。
//
// 使用场景：业务侧自己包了一层 helper（如 trace.StartHTTPSpan）想要在
// disabled 时直接返回原 ctx 与一个空 span，避免 nil 检查。
func NoopSpan(ctx context.Context) (context.Context, oteltrace.Span) {
	if Enabled() {
		// 即使 enabled，调用方传 nil ctx 也保护一下
		if ctx == nil {
			ctx = context.Background()
		}
		return ctx, oteltrace.SpanFromContext(ctx)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, oteltrace.SpanFromContext(ctx)
}
