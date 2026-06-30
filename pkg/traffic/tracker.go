package traffic

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

// direction 流量方向。0=上行（设备→服务），1=下行（服务→设备）。
type direction int

const (
	dirUp   direction = 0
	dirDown direction = 1
)

// counterPair 单设备的上下行字节累加器，atomic 原子操作。
type counterPair struct {
	up   int64 // 当前周期累计上行字节
	down int64 // 当前周期累计下行字节
}

// tracker 全局流量追踪器。
//
// 并发模型：
//   - whitelist / counters：Init 后只读，无锁
//   - counterPair.up/down：所有写操作通过 atomic，flush 时用 SwapInt64 取值并清零
//   - flushTicker 由单 goroutine 驱动，与业务侧 atomic.AddInt64 互不阻塞
type tracker struct {
	cfg       Config
	whitelist map[string]struct{}     // 白名单（Init 后只读）
	counters  map[string]*counterPair // 启动时按白名单预创建，业务侧只 atomic 修改字段

	flushTicker *time.Ticker
	stopChan    chan struct{}
	stopped     chan struct{}
}

func newTracker(cfg Config, whitelist map[string]struct{}) *tracker {
	counters := make(map[string]*counterPair, len(whitelist))
	for no := range whitelist {
		counters[no] = &counterPair{}
	}
	return &tracker{
		cfg:       cfg,
		whitelist: whitelist,
		counters:  counters,
		stopChan:  make(chan struct{}),
		stopped:   make(chan struct{}),
	}
}

// isTracked 判断设备号是否在白名单。Init 后 whitelist 只读，无锁安全。
func (t *tracker) isTracked(deviceNo string) bool {
	_, ok := t.whitelist[deviceNo]
	return ok
}

// addUp 累加上行字节。设备不在白名单时静默忽略。
func (t *tracker) addUp(deviceNo string, n int64) {
	if p, ok := t.counters[deviceNo]; ok {
		atomic.AddInt64(&p.up, n)
	}
}

// addDown 累加下行字节。设备不在白名单时静默忽略。
func (t *tracker) addDown(deviceNo string, n int64) {
	if p, ok := t.counters[deviceNo]; ok {
		atomic.AddInt64(&p.down, n)
	}
}

// start 启动后台 flush ticker。
func (t *tracker) start() {
	t.flushTicker = time.NewTicker(t.cfg.FlushInterval)
	go t.flushLoop()
}

// stop 优雅停止：发停止信号 → 等 flushLoop 退出 → 退出前会再 flush 一次残留。
func (t *tracker) stop() {
	close(t.stopChan)
	<-t.stopped
}

// flushLoop 后台 goroutine，按 FlushInterval 周期把内存增量推到 Redis。
func (t *tracker) flushLoop() {
	defer close(t.stopped)
	defer t.flushTicker.Stop()
	for {
		select {
		case <-t.stopChan:
			// 优雅退出：最后 flush 一次防止残留内存丢失
			t.flushOnce(context.Background())
			return
		case <-t.flushTicker.C:
			t.flushOnce(context.Background())
		}
	}
}

// pendingItem flush 时的中间数据，按设备号聚合 swap 出来的累加值。
type pendingItem struct {
	deviceNo string
	up, down int64
}

// flushOnce 把内存累加值 swap 到 0，并 Pipeline HINCRBY 到 Redis。
//
// 失败回滚策略：HINCRBY 失败时把 swap 走的值用 atomic.AddInt64 加回内存，
// 等下次 flush 重试。是幂等的（即使期间有新累加也只是再加上去）。
func (t *tracker) flushOnce(ctx context.Context) {
	// 1. swap 出所有非零累加值
	var pending []pendingItem
	for deviceNo, pair := range t.counters {
		up := atomic.SwapInt64(&pair.up, 0)
		down := atomic.SwapInt64(&pair.down, 0)
		if up > 0 || down > 0 {
			pending = append(pending, pendingItem{deviceNo, up, down})
		}
	}
	if len(pending) == 0 {
		return
	}

	// 2. Pipeline HINCRBY + Expire 续 TTL
	hourKey := t.cfg.KeyPrefix + time.Now().Format("2006010215")
	pipe := t.cfg.Redis.Pipeline()
	for _, item := range pending {
		if item.up > 0 {
			pipe.HIncrBy(ctx, hourKey, buildField(item.deviceNo, t.cfg.ServiceName, dirUp), item.up)
		}
		if item.down > 0 {
			pipe.HIncrBy(ctx, hourKey, buildField(item.deviceNo, t.cfg.ServiceName, dirDown), item.down)
		}
	}
	// 每次 flush 都续 TTL，保证活跃小时的 Key 不会被误清
	pipe.Expire(ctx, hourKey, t.cfg.HourKeyTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		// 失败回滚：把 swap 走的值加回内存（atomic 加法幂等）
		for _, item := range pending {
			if p, ok := t.counters[item.deviceNo]; ok {
				atomic.AddInt64(&p.up, item.up)
				atomic.AddInt64(&p.down, item.down)
			}
		}
		log.Printf("[traffic] flush to redis failed (rolled back to memory): %v", err)
	}
}

// buildField 构造 Redis Hash field：{deviceNo}|{service}|{up|down}
// 用 "|" 分隔（设备号/服务名都不可能含 |）
func buildField(deviceNo, service string, dir direction) string {
	d := "up"
	if dir == dirDown {
		d = "down"
	}
	return deviceNo + "|" + service + "|" + d
}
