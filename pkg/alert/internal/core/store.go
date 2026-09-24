package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrStoreUnavailable = errors.New("p0alert: state store unavailable")
	ErrCapacityExceeded = errors.New("p0alert: store capacity exceeded")
)

// ZMember 有序集合成员。
type ZMember struct {
	Member string
	Score  float64
}

// StateStore 多实例阶段标记与抢占。
// Redis 挂时降级本机：监控自检，不回滚业务、不堵 TCP。
type StateStore interface {
	Get(ctx context.Context, key string) (string, error)
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	ZAdd(ctx context.Context, key string, score float64, member string) error
	ZRangeByScore(ctx context.Context, key string, min, max float64, limit int) ([]ZMember, error)
	ZRem(ctx context.Context, key string, members ...string) error
}
