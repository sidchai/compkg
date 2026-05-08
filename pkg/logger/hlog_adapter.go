package logger

import (
	"context"
	"fmt"
	"io"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"go.uber.org/zap/zapcore"
)

// hlogAdapter 实现 hlog.FullLogger，把 hertz/hlog 调用桥接到 compkg/logger 的统一驱动。
//
// 业务侧 `hlog.Infof / hlog.CtxInfof / ...` 不需要任何修改；ctx 中携带的 BizCtx + trace 自动写入字段。
type hlogAdapter struct{}

// HlogAdapter 返回 hlog.FullLogger 实例；先 Bootstrap 再调用此函数。
//
//	hlog.SetLogger(logger.HlogAdapter())
func HlogAdapter() hlog.FullLogger { return &hlogAdapter{} }

// 编译期接口校验
var _ hlog.FullLogger = (*hlogAdapter)(nil)

// ---------- 不带 ctx 的输出 ----------

func (a *hlogAdapter) Trace(v ...interface{})  { a.log(nil, zapcore.DebugLevel, fmt.Sprint(v...)) }
func (a *hlogAdapter) Debug(v ...interface{})  { a.log(nil, zapcore.DebugLevel, fmt.Sprint(v...)) }
func (a *hlogAdapter) Info(v ...interface{})   { a.log(nil, zapcore.InfoLevel, fmt.Sprint(v...)) }
func (a *hlogAdapter) Notice(v ...interface{}) { a.log(nil, zapcore.WarnLevel, fmt.Sprint(v...)) }
func (a *hlogAdapter) Warn(v ...interface{})   { a.log(nil, zapcore.WarnLevel, fmt.Sprint(v...)) }
func (a *hlogAdapter) Error(v ...interface{})  { a.log(nil, zapcore.ErrorLevel, fmt.Sprint(v...)) }
func (a *hlogAdapter) Fatal(v ...interface{})  { a.log(nil, zapcore.FatalLevel, fmt.Sprint(v...)) }

func (a *hlogAdapter) Tracef(format string, v ...interface{}) {
	a.log(nil, zapcore.DebugLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) Debugf(format string, v ...interface{}) {
	a.log(nil, zapcore.DebugLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) Infof(format string, v ...interface{}) {
	a.log(nil, zapcore.InfoLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) Noticef(format string, v ...interface{}) {
	a.log(nil, zapcore.WarnLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) Warnf(format string, v ...interface{}) {
	a.log(nil, zapcore.WarnLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) Errorf(format string, v ...interface{}) {
	a.log(nil, zapcore.ErrorLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) Fatalf(format string, v ...interface{}) {
	a.log(nil, zapcore.FatalLevel, fmt.Sprintf(format, v...))
}

// ---------- 带 ctx 的输出（自动注入 trace / biz） ----------

func (a *hlogAdapter) CtxTracef(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.DebugLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) CtxDebugf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.DebugLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) CtxInfof(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.InfoLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) CtxNoticef(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.WarnLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) CtxWarnf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.WarnLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) CtxErrorf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.ErrorLevel, fmt.Sprintf(format, v...))
}
func (a *hlogAdapter) CtxFatalf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, zapcore.FatalLevel, fmt.Sprintf(format, v...))
}

// SetLevel 兼容 hlog.FullLogger 接口；映射到 SetLevelDynamic。
func (a *hlogAdapter) SetLevel(lv hlog.Level) {
	SetLevelDynamic(hlogLevelToLevel(lv))
}

// SetOutput 留空；compkg/logger 通过 BootstrapOptions 配置 sinks，不允许运行时换 io.Writer。
func (a *hlogAdapter) SetOutput(w io.Writer) {
	// no-op: 输出位置由 Bootstrap 决定。
}

// hlogLevelToLevel hlog 级别 -> compkg/logger 级别。
func hlogLevelToLevel(lv hlog.Level) Level {
	switch lv {
	case hlog.LevelTrace:
		return LevelTrace
	case hlog.LevelDebug:
		return LevelDebug
	case hlog.LevelInfo:
		return LevelInfo
	case hlog.LevelNotice:
		return LevelNotice
	case hlog.LevelWarn:
		return LevelWarn
	case hlog.LevelError:
		return LevelError
	case hlog.LevelFatal:
		return LevelFatal
	default:
		return LevelInfo
	}
}

// log 内部统一出口：构造 Entry 并写日志。
func (a *hlogAdapter) log(ctx context.Context, lvl zapcore.Level, msg string) {
	d := getDriver()
	if d == nil {
		return
	}
	if !d.sampler.allow(lvl) {
		return
	}
	if !d.atomicLvl.Enabled(lvl) {
		return
	}
	e := Entry{d: d, ctx: ctx}
	msg = applyMessageSanitize(d.rules, msg)
	fields := e.buildFields()
	if ce := d.logger.Check(lvl, msg); ce != nil {
		ce.Write(fields...)
	}
}
