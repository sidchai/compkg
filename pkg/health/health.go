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
	"encoding/json"
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

	// Meta 实例元信息（如 buildID/commit/startTime/pid/host）。
	// 仅当 Store 实现了 MetaStore 且 Meta 非空时上报。心跳每轮幂等覆盖写入。
	Meta map[string]string

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

// MetaStore 是 Store 的可选扩展，用于上报/读取实例元信息（如版本指纹、启动时间、pid）。
//
// 设计为独立可选接口而非并入 Store，是为了向后兼容：未实现该接口的旧 Store
// 仍能正常编译与运行（只是没有元信息能力）。Reporter/Reader 通过类型断言探测。
//
// 数据模型：
//
//	key   = {prefix}:health:meta:{service}   -> Hash
//	field = instance_id（与 ZSet member 一一对应）
//	value = 调用方自定义字符串（建议 JSON，存 buildID/commit/startTime/pid/host 等）
type MetaStore interface {
	HSet(ctx context.Context, key, field, value string) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HDel(ctx context.Context, key string, fields ...string) error
	// ZRangeByScoreWithScores 返回 [min,max] 区间内的成员及其 score（用于读取实例明细与 meta 对账）。
	ZRangeByScoreWithScores(ctx context.Context, key, min, max string) ([]ScoredMember, error)
}

// ScoredMember ZSet 成员及其 score（score 为最近一次心跳的 unix 秒）。
type ScoredMember struct {
	Member string
	Score  float64
}

// metaKeyFor 返回某个服务对应元信息 Hash 的 key。
func metaKeyFor(prefix, service string) string {
	if prefix == "" {
		prefix = defaultKeyPrefix
	}
	return fmt.Sprintf("%s:health:meta:%s", prefix, service)
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
			// 同步清理元信息，避免 Hash 残留孤儿字段。
			if ms, ok := store.(MetaStore); ok {
				_ = ms.HDel(ctx, metaKeyFor(r.Prefix, r.Service), r.Instance)
			}
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

	// 上报实例元信息（仅当 Store 支持 MetaStore 且配置了 Meta）。
	// 与 ZSet 心跳分离，写失败不影响存活判定。
	r.reportMeta(beatCtx, store)
}

// reportMeta 将 Meta 以 JSON 写入元信息 Hash；Store 不支持或 Meta 为空时静默跳过。
func (r *Reporter) reportMeta(ctx context.Context, store Store) {
	if len(r.Meta) == 0 {
		return
	}
	ms, ok := store.(MetaStore)
	if !ok {
		return
	}
	payload, err := json.Marshal(r.Meta)
	if err != nil {
		if r.OnError != nil {
			r.OnError(fmt.Errorf("health meta marshal: %w", err))
		}
		return
	}
	if err := ms.HSet(ctx, metaKeyFor(r.Prefix, r.Service), r.Instance, string(payload)); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("health meta hset: %w", err))
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

// InstanceMeta 单个实例的运行明细：心跳时间 + 上报的元信息。
type InstanceMeta struct {
	Instance string            // 实例 ID（hostname-pid-random）
	LastBeat time.Time         // 最近一次心跳时间
	Alive    bool              // 是否在 AliveWindow 内
	Meta     map[string]string // 实例上报的元信息（buildID/commit/startTime/pid/host 等），可能为 nil
}

// Instances 返回某服务全部实例的运行明细（含 meta），按是否存活标记。
//
// 以 ZSet 为存活权威来源（score=心跳时间），meta Hash 仅作信息补充：
// 遍历 ZSet 成员，再用 Hash 里对应 field 的 JSON 填充 Meta；Hash 缺失不影响存活判定。
// 要求 Store 实现 MetaStore，否则返回错误（调用方应降级为 Stat）。
func (rd *Reader) Instances(ctx context.Context, service string) ([]InstanceMeta, error) {
	store := rd.store()
	if store == nil {
		return nil, errors.New("health: nil redis client")
	}
	ms, ok := store.(MetaStore)
	if !ok {
		return nil, errors.New("health: store does not support MetaStore")
	}
	if rd.AliveWindow <= 0 {
		rd.AliveWindow = defaultAliveWindow
	}
	now := time.Now().Unix()
	aliveFrom := now - int64(rd.AliveWindow/time.Second)

	// 取全部成员（含历史未清理的），用 score 自行判定 alive，便于报告展示离线实例。
	members, err := ms.ZRangeByScoreWithScores(ctx, keyFor(rd.Prefix, service), "-inf", "+inf")
	if err != nil {
		return nil, fmt.Errorf("health instances zrange: %w", err)
	}
	metaMap, err := ms.HGetAll(ctx, metaKeyFor(rd.Prefix, service))
	if err != nil {
		// meta 读失败不致命：仍返回实例存活信息，Meta 置空。
		metaMap = map[string]string{}
	}

	result := make([]InstanceMeta, 0, len(members))
	for _, m := range members {
		im := InstanceMeta{
			Instance: m.Member,
			LastBeat: time.Unix(int64(m.Score), 0),
			Alive:    int64(m.Score) >= aliveFrom,
		}
		if raw, exists := metaMap[m.Member]; exists && raw != "" {
			parsed := map[string]string{}
			if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
				im.Meta = parsed
			}
		}
		result = append(result, im)
	}
	return result, nil
}
