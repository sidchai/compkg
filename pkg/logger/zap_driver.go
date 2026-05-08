package logger

import (
	"fmt"
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// driver 内部 zap 驱动。
//
// 字段固定层级：
//
//	ts / level / msg / caller / service / env / version / host
//	trace_id / span_id / device_no / account_id / msg_id / user_id / instance_id（按 ctx 写入）
//	其他用户字段通过 Entry.With(...) 注入
type driver struct {
	logger    *zap.Logger
	atomicLvl zap.AtomicLevel
	closers   []io.Closer
	rules     []SanitizeRule
	sampler   *sampler
	opts      BootstrapOptions
}

// buildDriver 按 BootstrapOptions 构造 driver。
func buildDriver(opts BootstrapOptions) (*driver, error) {
	if opts.ServiceName == "" {
		return nil, fmt.Errorf("logger: ServiceName is required")
	}
	opts.applyDefaults()

	host := opts.Host
	if host == "" {
		if h, err := os.Hostname(); err == nil {
			host = h
		}
	}
	opts.Host = host

	atomicLvl := zap.NewAtomicLevelAt(toZapLevel(opts.Level))

	encoder := buildEncoder(opts.Format)

	var (
		cores   []zapcore.Core
		closers []io.Closer
	)

	// 文件 sink
	if opts.File.Path != "" {
		lj := &lumberjack.Logger{
			Filename:   opts.File.Path,
			MaxSize:    opts.File.MaxSizeMB,
			MaxBackups: opts.File.MaxBackups,
			MaxAge:     opts.File.MaxAgeDays,
			Compress:   opts.File.Compress,
			LocalTime:  opts.File.LocalTime,
		}
		ws := zapcore.AddSync(lj)
		cores = append(cores, zapcore.NewCore(encoder, ws, atomicLvl))
		closers = append(closers, lj)
	}

	// stdout sink
	if opts.EnableStdout || opts.File.Path == "" {
		// 缺省至少有一个 sink；若两者都没配置时用 stdout 兜底
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), atomicLvl))
	}

	core := zapcore.NewTee(cores...)

	zapOpts := []zap.Option{}
	if opts.AddCaller {
		zapOpts = append(zapOpts, zap.AddCaller())
		// CallerSkip：默认 Entry 路径需跳 1 层（Entry 的 Info/Debug/...）；
		// 通过 hlog 适配走另一条路径，调用方再传 callerSkip 调整。
		if opts.CallerSkip > 0 {
			zapOpts = append(zapOpts, zap.AddCallerSkip(opts.CallerSkip))
		}
	}
	zapOpts = append(zapOpts, zap.AddStacktrace(zapcore.ErrorLevel))

	zl := zap.New(core, zapOpts...)

	// 固定字段：service / env / version / host
	fixed := []zap.Field{zap.String("service", opts.ServiceName)}
	if opts.Env != "" {
		fixed = append(fixed, zap.String("env", opts.Env))
	}
	if opts.Version != "" {
		fixed = append(fixed, zap.String("version", opts.Version))
	}
	if host != "" {
		fixed = append(fixed, zap.String("host", host))
	}
	zl = zl.With(fixed...)

	rules := opts.SanitizeRules
	if rules == nil {
		rules = DefaultSanitizeRules()
	}

	return &driver{
		logger:    zl,
		atomicLvl: atomicLvl,
		closers:   closers,
		rules:     rules,
		sampler:   newSampler(opts.SampleEveryN),
		opts:      opts,
	}, nil
}

// buildEncoder 构造 zapcore 编码器。
func buildEncoder(format string) zapcore.Encoder {
	cfg := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "ts",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if format == "console" {
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return zapcore.NewConsoleEncoder(cfg)
	}
	return zapcore.NewJSONEncoder(cfg)
}

// toZapLevel 将 hertz 风格 Level 映射到 zapcore.Level。
func toZapLevel(lv Level) zapcore.Level {
	switch lv {
	case LevelTrace, LevelDebug:
		return zapcore.DebugLevel
	case LevelInfo:
		return zapcore.InfoLevel
	case LevelNotice, LevelWarn:
		return zapcore.WarnLevel
	case LevelError:
		return zapcore.ErrorLevel
	case LevelFatal:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// setLevel 运行时切换日志级别。
func (d *driver) setLevel(lv Level) {
	d.atomicLvl.SetLevel(toZapLevel(lv))
}

// sync 刷写所有 buffer。
func (d *driver) sync() error {
	if d.logger == nil {
		return nil
	}
	// zap 的 Sync 在 stdout 上可能返回 EINVAL，业务层无需关心。
	_ = d.logger.Sync()
	return nil
}

// shutdown 关闭文件等可关闭资源。
func (d *driver) shutdown() error {
	_ = d.sync()
	var firstErr error
	for _, c := range d.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
