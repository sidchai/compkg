package trace

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"
)

// GRPCServerHandler 返回 grpc.StatsHandler，用于 grpc.NewServer(grpc.StatsHandler(...))。
//
// 用法（scheduler 服务端）：
//
//	srv := grpc.NewServer(grpc.StatsHandler(trace.GRPCServerHandler()))
//
// 自动从 metadata 读取 traceparent；命名 "grpc.{service}/{method}"。
func GRPCServerHandler() stats.Handler {
	return otelgrpc.NewServerHandler()
}

// GRPCClientHandler 返回 grpc.StatsHandler，用于 grpc.DialContext(grpc.WithStatsHandler(...))。
//
// 用法（业务调用 scheduler 时）：
//
//	conn, err := grpc.DialContext(ctx, addr,
//	    grpc.WithStatsHandler(trace.GRPCClientHandler()),
//	    ...,
//	)
func GRPCClientHandler() stats.Handler {
	return otelgrpc.NewClientHandler()
}
