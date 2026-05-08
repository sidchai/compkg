package logger

import (
	"sync/atomic"

	"go.uber.org/zap/zapcore"
)

// sampler 高频日志采样：每 N 条放行 1 条。仅作用于 Debug/Info；Warn 及以上永不采样。
type sampler struct {
	everyN int64
	count  atomic.Int64
}

func newSampler(everyN int) *sampler {
	if everyN <= 1 {
		return nil
	}
	return &sampler{everyN: int64(everyN)}
}

// allow 是否放行此条日志。
func (s *sampler) allow(lvl zapcore.Level) bool {
	if s == nil || s.everyN <= 1 {
		return true
	}
	if lvl >= zapcore.WarnLevel {
		return true
	}
	n := s.count.Add(1)
	return n%s.everyN == 1
}
