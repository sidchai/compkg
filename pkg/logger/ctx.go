package logger

import (
	"context"
	"sync/atomic"
)

// BizCtxKeys 业务上下文字段。一次绑定，整条调用链自动写入每条日志。
//
// 字段命名约定（snake_case，写入日志时使用相同 key）：
//   - device_no
//   - account_id
//   - msg_id
//   - user_id
//   - instance_id
type BizCtxKeys struct {
	DeviceNo   string
	AccountId  int64
	MsgId      string
	UserId     int64
	InstanceId string
}

type bizCtxKey struct{}

// WithBizCtx 把业务字段绑定到 ctx。
//
// 多次调用时后绑定的字段覆盖前者；空值字段不覆盖已有值（语义：增量补充）。
func WithBizCtx(ctx context.Context, k BizCtxKeys) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	prev := GetBizCtx(ctx)
	merged := prev
	if k.DeviceNo != "" {
		merged.DeviceNo = k.DeviceNo
	}
	if k.AccountId != 0 {
		merged.AccountId = k.AccountId
	}
	if k.MsgId != "" {
		merged.MsgId = k.MsgId
	}
	if k.UserId != 0 {
		merged.UserId = k.UserId
	}
	if k.InstanceId != "" {
		merged.InstanceId = k.InstanceId
	}
	return context.WithValue(ctx, bizCtxKey{}, merged)
}

// GetBizCtx 从 ctx 取出业务字段；未绑定则返回零值。
func GetBizCtx(ctx context.Context) BizCtxKeys {
	if ctx == nil {
		return BizCtxKeys{}
	}
	v := ctx.Value(bizCtxKey{})
	if v == nil {
		return BizCtxKeys{}
	}
	if k, ok := v.(BizCtxKeys); ok {
		return k
	}
	return BizCtxKeys{}
}

// TraceExtractor 从 ctx 提取 traceId / spanId。
//
// 默认实现返回空（即不写 trace 字段）；RFC-07 接入 OTel 时通过 SetTraceExtractor 注入：
//
//	import oteltrace "go.opentelemetry.io/otel/trace"
//	logger.SetTraceExtractor(func(ctx context.Context) (string, string) {
//	    sc := oteltrace.SpanContextFromContext(ctx)
//	    if !sc.IsValid() { return "", "" }
//	    return sc.TraceID().String(), sc.SpanID().String()
//	})
type TraceExtractor func(ctx context.Context) (traceId, spanId string)

// 通过 atomic.Value 存储以支持运行时替换。
var traceExtractor atomic.Value // TraceExtractor

func init() {
	traceExtractor.Store(TraceExtractor(func(context.Context) (string, string) { return "", "" }))
}

// SetTraceExtractor 注册全局 trace 抽取器；多次调用以最后一次为准。
func SetTraceExtractor(fn TraceExtractor) {
	if fn == nil {
		return
	}
	traceExtractor.Store(fn)
}

func extractTrace(ctx context.Context) (string, string) {
	if ctx == nil {
		return "", ""
	}
	fn, _ := traceExtractor.Load().(TraceExtractor)
	if fn == nil {
		return "", ""
	}
	return fn(ctx)
}
