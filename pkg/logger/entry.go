package logger

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Entry 链式结构化日志构造器。
//
//	logger.Ctx(ctx).
//	    With("instruction_type", t).
//	    With("elapsed_ms", ms).
//	    WithErr(err).
//	    Error("dispatch fail")
//
// 每条日志会自动注入：trace_id / span_id / device_no / account_id / msg_id / user_id / instance_id。
type Entry struct {
	d      *driver
	ctx    context.Context
	fields []zap.Field
	err    error
}

// Ctx 返回带 ctx 字段的 Entry。
//
// 必须先调用 Bootstrap；未 Bootstrap 时返回的 Entry 调用方法不 panic 也不输出（no-op）。
func Ctx(ctx context.Context) Entry {
	return Entry{d: getDriver(), ctx: ctx}
}

// With 追加结构化字段。
func (e Entry) With(key string, value any) Entry {
	if e.d == nil {
		return e
	}
	value = applySanitize(e.d.rules, key, value)
	e.fields = append(e.fields, zap.Any(key, value))
	return e
}

// WithErr 追加 error 字段（key="error"）。
func (e Entry) WithErr(err error) Entry {
	if err == nil {
		return e
	}
	e.err = err
	e.fields = append(e.fields, zap.Error(err))
	return e
}

// Debug / Info / Warn / Error 输出对应级别。
func (e Entry) Debug(msg string) { e.log(zapcore.DebugLevel, msg) }
func (e Entry) Info(msg string)  { e.log(zapcore.InfoLevel, msg) }
func (e Entry) Warn(msg string)  { e.log(zapcore.WarnLevel, msg) }
func (e Entry) Error(msg string) { e.log(zapcore.ErrorLevel, msg) }

// Fatal 输出后调用 os.Exit(1)（zap 内部行为）。
func (e Entry) Fatal(msg string) { e.log(zapcore.FatalLevel, msg) }

func (e Entry) log(lvl zapcore.Level, msg string) {
	if e.d == nil {
		return
	}
	if !e.d.sampler.allow(lvl) {
		return
	}
	if !e.d.atomicLvl.Enabled(lvl) {
		return
	}
	msg = applyMessageSanitize(e.d.rules, msg)
	fields := e.buildFields()
	if ce := e.d.logger.Check(lvl, msg); ce != nil {
		ce.Write(fields...)
	}
}

// buildFields 把 ctx 中的 trace / biz 字段拼接到 e.fields 之前。
func (e Entry) buildFields() []zap.Field {
	out := make([]zap.Field, 0, len(e.fields)+8)

	if e.ctx != nil {
		if traceId, spanId := extractTrace(e.ctx); traceId != "" {
			out = append(out, zap.String("trace_id", traceId))
			if spanId != "" {
				out = append(out, zap.String("span_id", spanId))
			}
		}
		biz := GetBizCtx(e.ctx)
		if biz.DeviceNo != "" {
			out = append(out, zap.String("device_no", biz.DeviceNo))
		}
		if biz.AccountId != 0 {
			out = append(out, zap.Int64("account_id", biz.AccountId))
		}
		if biz.MsgId != "" {
			out = append(out, zap.String("msg_id", biz.MsgId))
		}
		if biz.UserId != 0 {
			out = append(out, zap.Int64("user_id", biz.UserId))
		}
		if biz.InstanceId != "" {
			out = append(out, zap.String("instance_id", biz.InstanceId))
		}
	}

	out = append(out, e.fields...)
	return out
}
