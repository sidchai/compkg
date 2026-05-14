// Package health 提供基于 Redis ZSet 的服务实例心跳上报与活实例统计能力。
//
// 数据模型：
//
//	key   = {prefix}:health:instance:{service}    -> ZSet
//	member = instance_id（建议 hostname+pid+random）
//	score  = 最近一次心跳的 unix 秒
//
// Reporter 周期性 ZADD 自己的 instance_id；同时按 staleAfter 清理过期 member，避免无界增长。
// Reader 提供按 service 维度统计活实例 / 总实例的能力。
package health

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

const (
	defaultKeyPrefix    = "compkg"
	defaultInterval     = 10 * time.Second
	defaultAliveWindow  = 30 * time.Second
	defaultStaleCleanup = 5 * time.Minute
	defaultOpTimeout    = 3 * time.Second
)

// keyFor 返回某个服务对应的 ZSet key。
func keyFor(prefix, service string) string {
	if prefix == "" {
		prefix = defaultKeyPrefix
	}
	return fmt.Sprintf("%s:health:instance:%s", prefix, service)
}

// Reporter 在后台周期性向 Redis 上报当前进程的心跳。
//
// 必须设置 Store / Service / Instance；其它字段为零值时使用默认值。
// 调用 Start 启动后台 goroutine，调用 Stop 主动注销自己（ZREM）。
type Reporter struct {
	Store    Store
	Prefix   string        // 可选，默认为 "compkg"
	Service  string        // 必填，服务名
	Instance string        // 必填，实例唯一标识
	Interval time.Duration // 心跳间隔，默认 10s
	// StaleCleanup 比 Reader.AliveWindow 更大；超过该阈值的 member 视为彻底失联，由 Reporter 顺手清理。
	StaleCleanup time.Duration

	OnError func(err error) // 可选错误回调；nil 时静默

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

type Store interface {
	ZAdd(ctx context.Context, key string, score float64, member interface{}) error
	ZRemRangeByScore(ctx context.Context, key, min, max string) error
	ZRem(ctx context.Context, key string, member interface{}) error
	ZCount(ctx context.Context, key, min, max string) (int64, error)
	ZCard(ctx context.Context, key string) (int64, error)
}

func (r *Reporter) store() Store {
	return r.Store
}

// Start 启动后台心跳 goroutine。重复调用安全：第一次之后的调用会被忽略。
func (r *Reporter) Start(ctx context.Context) error {
	store := r.store()
	if store == nil {
		return errors.New("health: nil redis client")
	}
	if r.Service == "" || r.Instance == "" {
		return errors.New("health: empty service or instance")
	}
	if r.Interval <= 0 {
		r.Interval = defaultInterval
	}
	if r.StaleCleanup <= 0 {
		r.StaleCleanup = defaultStaleCleanup
	}

	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})

	// 立即上报一次，保证 /statistics/health 不必等 Interval。
	r.beat(ctx)

	go func() {
		defer close(r.doneCh)
		ticker := time.NewTicker(r.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-ticker.C:
				r.beat(ctx)
			}
		}
	}()
	return nil
}

// Stop 停止心跳并从 ZSet 中移除当前实例（best effort）。
func (r *Reporter) Stop(ctx context.Context) {
	r.stopOnce.Do(func() {
		if r.stopCh != nil {
			close(r.stopCh)
		}
		if r.doneCh != nil {
			<-r.doneCh
		}
		if store := r.store(); store != nil {
			_ = store.ZRem(ctx, keyFor(r.Prefix, r.Service), r.Instance)
		}
	})
}

func (r *Reporter) beat(ctx context.Context) {
	store := r.store()
	if store == nil {
		if r.OnError != nil {
			r.OnError(errors.New("health beat: nil redis client"))
		}
		return
	}
	beatCtx, cancel := context.WithTimeout(ctx, defaultOpTimeout)
	defer cancel()
	key := keyFor(r.Prefix, r.Service)
	now := time.Now().Unix()

	if err := store.ZAdd(beatCtx, key, float64(now), r.Instance); err != nil {
		if r.OnError != nil {
			r.OnError(fmt.Errorf("health beat: %w", err))
		}
		return
	}
	staleBefore := now - int64(r.StaleCleanup/time.Second)
	if err := store.ZRemRangeByScore(beatCtx, key, "-inf", strconv.FormatInt(staleBefore, 10)); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("health beat: %w", err))
	}
}

// Reader 提供按服务名查询活实例 / 总实例数。
type Reader struct {
	Store  Store
	Prefix string // 与 Reporter 保持一致
	// AliveWindow：score >= now - AliveWindow 的 member 视为活实例。默认 30s。
	AliveWindow time.Duration
}

func (rd *Reader) store() Store {
	return rd.Store
}

// ServiceStat 单个服务的实例统计。
type ServiceStat struct {
	Service string
	Running int64 // 活实例数
	Total   int64 // ZSet 总成员数（含未到 StaleCleanup 的历史实例）
}

// Stat 查询单个服务的实例状态。
func (rd *Reader) Stat(ctx context.Context, service string) (ServiceStat, error) {
	store := rd.store()
	if store == nil {
		return ServiceStat{Service: service}, errors.New("health: nil redis client")
	}
	if rd.AliveWindow <= 0 {
		rd.AliveWindow = defaultAliveWindow
	}
	key := keyFor(rd.Prefix, service)
	now := time.Now().Unix()
	aliveFrom := now - int64(rd.AliveWindow/time.Second)

	running, err := store.ZCount(ctx, key, strconv.FormatInt(aliveFrom, 10), "+inf")
	if err != nil {
		return ServiceStat{Service: service}, err
	}
	total, err := store.ZCard(ctx, key)
	if err != nil {
		return ServiceStat{Service: service}, err
	}
	return ServiceStat{
		Service: service,
		Running: running,
		Total:   total,
	}, nil
}

// StatAll 批量查询多个服务的实例状态。任一服务查询失败不会中断其余服务。
func (rd *Reader) StatAll(ctx context.Context, services []string) ([]ServiceStat, error) {
	result := make([]ServiceStat, 0, len(services))
	var firstErr error
	for _, svc := range services {
		s, err := rd.Stat(ctx, svc)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		result = append(result, s)
	}
	return result, firstErr
}

// Aggregate 汇总多个服务的 Running / Total。
func Aggregate(stats []ServiceStat) (running, total int64) {
	for _, s := range stats {
		running += s.Running
		total += s.Total
	}
	return
}
