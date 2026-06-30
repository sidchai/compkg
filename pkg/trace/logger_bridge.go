package trace

import (
	"context"

	"github.com/sidchai/compkg/pkg/logger"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// registerLoggerBridge 注册 logger.SetTraceExtractor，把 OTel SpanContext 写进日志。
//
// 由 Bootstrap 内部调用；如果上层未引入 logger，编译期不会报错（同 module 内 import）。
//
// 副作用：每次 Bootstrap 都会覆盖一次注册，多次调用以最后一次为准。
// disabled 模式下也注册，使得跨服务收到 traceparent 时仍能记入日志。
func registerLoggerBridge() {
	logger.SetTraceExtractor(func(ctx context.Context) (string, string) {
		if ctx == nil {
			return "", ""
		}
		sc := oteltrace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return "", ""
		}
		return sc.TraceID().String(), sc.SpanID().String()
	})
}
