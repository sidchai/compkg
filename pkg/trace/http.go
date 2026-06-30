package trace

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// HTTP 入站中间件：使用 otelhttp.NewHandler 包装。
//
// 用法（标准 net/http）：
//
//	mux := http.NewServeMux()
//	mux.Handle("/record/start", recordStartHandler)
//	srv := &http.Server{Handler: trace.NewHTTPMiddleware("iot_cloud_platform_open")(mux)}
//
// 路由名（http.route attribute）由 otelhttp 默认根据 mux 模式提取。
// 框架（Hertz/Gin/Echo）请使用对应 instrumentation 包，本函数仅用于 net/http。
func NewHTTPMiddleware(serviceSpanPrefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, serviceSpanPrefix,
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				// span 命名规范（RFC §10.4）：http.{METHOD} {route}
				route := r.URL.Path
				if pattern, ok := r.Context().Value(httpRouteCtxKey{}).(string); ok && pattern != "" {
					route = pattern
				}
				return "http." + r.Method + " " + route
			}),
		)
	}
}

// 业务侧若使用了自定义 router 框架，可在 handler 中显式调用 SetHTTPRoute 把
// 模板路径写回 ctx，让 otelhttp 在 span name 中使用模板而非具体路径。
type httpRouteCtxKey struct{}

func SetHTTPRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, httpRouteCtxKey{}, route)
}

// NewHTTPClient 返回一个包装过 OTel 的 http.Client；自动注入 traceparent。
//
// transport 为 nil 时使用 http.DefaultTransport。
func NewHTTPClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{
		Transport: otelhttp.NewTransport(transport,
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return "http.client." + r.Method + " " + r.URL.Host
			}),
		),
	}
}

// StartHTTPClientSpan 用于业务自管 client 调用（如自定义 retry）想手动开 span 的场景。
//
// 返回的 ctx 已包含 span，调用方需在请求结束后 span.End()。
func StartHTTPClientSpan(ctx context.Context, method, host string) (context.Context, oteltrace.Span) {
	tr := Tracer("compkg/trace/http")
	c, span := tr.Start(ctx, "http.client."+method+" "+host,
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(method),
			attribute.String("server.address", host),
		),
	)
	return c, span
}
