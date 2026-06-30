package trace

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// TCPContextMap 是云侧 msgId → 下发时刻 SpanContext 的 TTL 映射。
// 用于 TCP ACK 回包续接 trace（RFC §5.3）。
//
// 设计要点：
//   - 不依赖 Redis：每个通讯节点本机维护，重启即丢失（业务结果不受影响，仅丢父子关系）
//   - 默认 TTL = max(valid_time, 5min)，上限 10min（建议值）
//   - 并发安全；通过分桶 + 单 sweeper goroutine 清理过期项
//   - 对外只暴露 Save / Load / Stats / Close 四个方法
type TCPContextMap struct {
	mu      sync.RWMutex
	items   map[string]*tcpEntry
	maxTTL  time.Duration
	sweepCh chan struct{}
	sweeper sync.Once
	closed  atomic.Bool
}

type tcpEntry struct {
	sc       oteltrace.SpanContext
	deviceNo string
	insType  string
	expireAt time.Time
}

// NewTCPContextMap 创建新映射。maxTTL 是单条记录最长存活时间，到期由 sweeper 清理。
//
// 推荐 maxTTL=10*time.Minute；通讯服务在写入时再用业务 valid_time 做更短上限。
func NewTCPContextMap(maxTTL time.Duration) *TCPContextMap {
	if maxTTL <= 0 {
		maxTTL = 10 * time.Minute
	}
	m := &TCPContextMap{
		items:   make(map[string]*tcpEntry),
		maxTTL:  maxTTL,
		sweepCh: make(chan struct{}),
	}
	m.sweeper.Do(func() {
		go m.runSweeper()
	})
	return m
}

// Save 把 ctx 中的 SpanContext 与 msgId 关联。
//
//   - 若 ctx 中无 valid SpanContext：仍写入空 entry，便于业务自查；
//     但 Load 时不会返回 valid context。
//   - ttl 为单次记录的存活上限；调用方应传 max(valid_time, 5min) 或最长 maxTTL。
func (m *TCPContextMap) Save(ctx context.Context, msgID string, ttl time.Duration, deviceNo, insType string) {
	if m == nil || m.closed.Load() || msgID == "" {
		return
	}
	if ttl <= 0 || ttl > m.maxTTL {
		ttl = m.maxTTL
	}
	sc := oteltrace.SpanContextFromContext(ctx)
	entry := &tcpEntry{
		sc:       sc,
		deviceNo: deviceNo,
		insType:  insType,
		expireAt: time.Now().Add(ttl),
	}
	m.mu.Lock()
	m.items[msgID] = entry
	m.mu.Unlock()
}

// Load 按 msgId 取出 ctx；ok=true 表示命中且 SpanContext valid。
//
// 取出后立即从 map 删除（一次性消费），避免重复 ACK 干扰下次链路。
func (m *TCPContextMap) Load(parent context.Context, msgID string) (ctx context.Context, deviceNo, insType string, ok bool) {
	if m == nil || m.closed.Load() || msgID == "" {
		return parent, "", "", false
	}
	if parent == nil {
		parent = context.Background()
	}
	m.mu.Lock()
	entry, found := m.items[msgID]
	if found {
		delete(m.items, msgID)
	}
	m.mu.Unlock()
	if !found {
		return parent, "", "", false
	}
	if time.Now().After(entry.expireAt) {
		return parent, "", "", false
	}
	if !entry.sc.IsValid() {
		return parent, entry.deviceNo, entry.insType, false
	}
	return oteltrace.ContextWithSpanContext(parent, entry.sc), entry.deviceNo, entry.insType, true
}

// Stats 返回当前映射条目数；用于上报 trace_context_map_size 指标。
func (m *TCPContextMap) Stats() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

// Close 停止 sweeper；通常只在进程退出时调用。
func (m *TCPContextMap) Close() {
	if m == nil || !m.closed.CompareAndSwap(false, true) {
		return
	}
	close(m.sweepCh)
}

func (m *TCPContextMap) runSweeper() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.sweepCh:
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

func (m *TCPContextMap) sweep() {
	now := time.Now()
	m.mu.Lock()
	for k, v := range m.items {
		if now.After(v.expireAt) {
			delete(m.items, k)
		}
	}
	m.mu.Unlock()
}
