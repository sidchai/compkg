// Package device 实现心跳五项规则：分片 map + TTL + 容量上限，单设备一把锁。
package device

import (
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

const (
	deviceShardCount = 64
	defaultDeviceCap = 100000
	defaultDeviceTTL = 45 * time.Minute
)

// Sample 心跳五项。设备号进 object 明文；dedup 仍用哈希，禁止当 Prometheus 标签。
type Sample struct {
	DeviceNo    string
	Temperature float64
	HasTemp     bool
	Battery     float64
	HasBattery  bool
	SDRemainMB  float64
	HasSD       bool
	WaitUpload  float64
	HasWait     bool
	Online      bool
	HasOnline   bool
	// Reconnect 本条是离线后重新连上（由业务标，禁止用离线日志回放冒充下线）。
	Reconnect bool
	At        time.Time
}

type deviceHit struct {
	subType    string
	firingText string
	resolveTxt string
}

type deviceState struct {
	mu            sync.Mutex
	lastSeen      time.Time
	lastOnline    bool
	hasLastOnline bool
	tempStreak    int
	tempOKStreak  int
	lastTemp      float64
	lastTempAt    time.Time
	battStreak    int
	battOKStreak  int
	lastBatt      float64
	lastBattAt    time.Time
	hasLastBatt   bool
	sdStreak      int
	sdOKStreak    int
	lastSD        float64
	lastSDAt      time.Time
	waitStreak    int
	waitOKStreak  int
	lastWait      float64
	lastWaitAt    time.Time
	reconnects    []time.Time
	active        map[string]bool
	everFired     bool
	lastFireAt    time.Time
	// daily 只限制 SD 不足和待上传积压。进程内先挡，避免每条心跳都打存储。
	daily map[string]dayCount
}

// Engine 分片 + TTL + 容量上限。单设备一把锁。
type Engine struct {
	shards   [deviceShardCount]*deviceShard
	exclude  map[string]struct{}
	capacity int
	ttl      time.Duration
	th       core.P0Thresholds
	store    core.StateStore
	total    atomic.Int64
	evalN    atomic.Uint64
}

type deviceShard struct {
	mu sync.Mutex
	m  map[string]*deviceState
}

// Result 一次心跳评估结果。
type Result struct {
	Fire     bool
	Remind   bool
	Resolve  bool
	Hits     []string
	FireText string
	ResText  string
	Object   string
	DedupKey string
}

// New 创建引擎。exclude 为运维/测试设备白名单，默认不得全局忽略。
func New(capacity int, exclude []string) *Engine {
	if capacity <= 0 {
		capacity = defaultDeviceCap
	}
	th := core.P0Thresholds{}
	th.ApplyDefaults()
	e := &Engine{
		exclude:  make(map[string]struct{}, len(exclude)),
		capacity: capacity,
		ttl:      defaultDeviceTTL,
		th:       th,
	}
	for _, d := range exclude {
		e.exclude[d] = struct{}{}
	}
	for i := 0; i < deviceShardCount; i++ {
		e.shards[i] = &deviceShard{m: make(map[string]*deviceState)}
	}
	return e
}

// SetStore 注入多实例状态。nil 时只靠本进程计数，重启后同一自然日可能再告一次。
func (e *Engine) SetStore(st core.StateStore) {
	if e == nil {
		return
	}
	e.store = st
}

// SetThresholds 用 YAML 覆盖数字阈值；≤0 的字段仍走默认。须在 Evaluate 前调用。
func (e *Engine) SetThresholds(th core.P0Thresholds) {
	if e == nil {
		return
	}
	th.ApplyDefaults()
	e.th = th
}

func (e *Engine) shardOf(id string) *deviceShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return e.shards[h.Sum32()%deviceShardCount]
}

func (e *Engine) gcShard(sh *deviceShard, now time.Time) {
	for k, st := range sh.m {
		st.mu.Lock()
		expired := now.Sub(st.lastSeen) > e.ttl
		st.mu.Unlock()
		if expired {
			delete(sh.m, k)
			e.total.Add(-1)
		}
	}
}

func (e *Engine) getOrCreate(deviceNo string, now time.Time) *deviceState {
	sh := e.shardOf(deviceNo)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if e.evalN.Add(1)%1024 == 0 {
		e.gcShard(sh, now)
	}
	if st, ok := sh.m[deviceNo]; ok {
		return st
	}
	// 超容量拒绝新设备，宁可漏监控也不能让 map 撑爆心跳路径。
	if int(e.total.Load()) >= e.capacity {
		return nil
	}
	st := &deviceState{active: make(map[string]bool)}
	sh.m[deviceNo] = st
	e.total.Add(1)
	return st
}

func (e *Engine) Size() int {
	n := 0
	for i := range e.shards {
		e.shards[i].mu.Lock()
		n += len(e.shards[i].m)
		e.shards[i].mu.Unlock()
	}
	return n
}

// Evaluate 由 Ingest worker 调用。两连接同时心跳同一设备：后写覆盖，只推一条。
func (e *Engine) Evaluate(s Sample) Result {
	if _, skip := e.exclude[s.DeviceNo]; skip {
		return Result{}
	}
	if s.At.IsZero() {
		s.At = time.Now()
	}
	st := e.getOrCreate(s.DeviceNo, s.At)
	if st == nil {
		return Result{}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeen = s.At

	var firingHits []deviceHit
	var resolvedSubs []string

	if s.HasTemp {
		if !e.adjacentTimes(st.lastTempAt, s.At) {
			st.tempStreak = 0
			st.tempOKStreak = 0
		}
		if s.Temperature > e.th.TempGT {
			st.tempStreak++
			st.tempOKStreak = 0
			if st.tempStreak >= 2 {
				firingHits = append(firingHits, deviceHit{
					subType:    core.SubTemperatureHigh,
					firingText: formatTempFire(s.Temperature, e.th.TempGT),
					resolveTxt: formatTempResolve(s.Temperature),
				})
			}
		} else {
			st.tempOKStreak++
			st.tempStreak = 0
			if st.active[core.SubTemperatureHigh] && st.tempOKStreak >= 2 {
				resolvedSubs = append(resolvedSubs, core.SubTemperatureHigh)
			}
		}
		st.lastTemp = s.Temperature
		st.lastTempAt = s.At
	}

	if s.HasBattery {
		// 相邻：设备时间间隔 ≤6 分钟。隔得更久只更新基准，不按跳变告（关机充电后再上线会从 20% 到 80%）。
		invalid := s.Battery < 0 || s.Battery > 100
		adjacent := e.adjacentTimes(st.lastBattAt, s.At) && st.hasLastBatt
		if !adjacent {
			st.battStreak = 0
			st.battOKStreak = 0
		}
		jump := adjacent && abs(s.Battery-st.lastBatt) > e.th.BatteryJump
		if invalid || jump {
			st.battStreak++
			st.battOKStreak = 0
			if st.battStreak >= 1 {
				text := formatBatteryJump(st.lastBatt, s.Battery, e.th.BatteryJump)
				if invalid {
					text = formatBatteryInvalid(s.Battery)
				}
				firingHits = append(firingHits, deviceHit{
					subType:    core.SubBatteryAbnormal,
					firingText: text,
					resolveTxt: "设备电量上报已经恢复正常",
				})
			}
		} else {
			st.battOKStreak++
			st.battStreak = 0
			if st.active[core.SubBatteryAbnormal] && st.battOKStreak >= 2 {
				resolvedSubs = append(resolvedSubs, core.SubBatteryAbnormal)
			}
		}
		st.lastBatt = s.Battery
		st.lastBattAt = s.At
		st.hasLastBatt = true
	}

	if s.HasSD {
		if !e.adjacentTimes(st.lastSDAt, s.At) {
			st.sdStreak = 0
			st.sdOKStreak = 0
		}
		if s.SDRemainMB < e.th.SDRemainMBLT {
			st.sdStreak++
			st.sdOKStreak = 0
			if st.sdStreak >= 2 {
				firingHits = append(firingHits, deviceHit{
					subType:    core.SubSDCardLow,
					firingText: formatSDFire(s.SDRemainMB, e.th.SDRemainMBLT),
					resolveTxt: formatSDResolve(s.SDRemainMB),
				})
			}
		} else {
			st.sdOKStreak++
			st.sdStreak = 0
			if st.active[core.SubSDCardLow] && st.sdOKStreak >= 2 {
				resolvedSubs = append(resolvedSubs, core.SubSDCardLow)
			}
		}
		st.lastSD = s.SDRemainMB
		st.lastSDAt = s.At
	}

	if s.HasWait {
		if !e.adjacentTimes(st.lastWaitAt, s.At) {
			st.waitStreak = 0
			st.waitOKStreak = 0
		}
		if s.WaitUpload > e.th.WaitUploadGT {
			st.waitStreak++
			st.waitOKStreak = 0
			if st.waitStreak >= 2 {
				firingHits = append(firingHits, deviceHit{
					subType:    core.SubUploadBacklog,
					firingText: formatWaitFire(s.WaitUpload, e.th.WaitUploadGT),
					resolveTxt: formatWaitResolve(s.WaitUpload),
				})
			}
		} else {
			st.waitOKStreak++
			st.waitStreak = 0
			if st.active[core.SubUploadBacklog] && st.waitOKStreak >= 2 {
				resolvedSubs = append(resolvedSubs, core.SubUploadBacklog)
			}
		}
		st.lastWait = s.WaitUpload
		st.lastWaitAt = s.At
	}

	if s.Reconnect {
		st.reconnects = append(st.reconnects, s.At)
	} else if s.HasOnline {
		// 兼容单测：在线状态从 false→true 计一次。生产请走 Reconnect，勿用离线回放当 false。
		if st.hasLastOnline && !st.lastOnline && s.Online {
			st.reconnects = append(st.reconnects, s.At)
		}
	}
	if s.HasOnline {
		st.lastOnline = s.Online
		st.hasLastOnline = true
	}
	cut := s.At.Add(-e.th.ReconnectWindow())
	kept := st.reconnects[:0]
	for _, t := range st.reconnects {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	st.reconnects = kept
	if len(st.reconnects) >= e.th.ReconnectCount {
		firingHits = append(firingHits, deviceHit{
			subType:    core.SubReconnectFrequent,
			firingText: formatReconnectFire(len(st.reconnects), e.th.ReconnectWindowSec, e.th.ReconnectCount),
			resolveTxt: "设备重新联网频率已经恢复正常",
		})
	} else if st.active[core.SubReconnectFrequent] && len(st.reconnects) == 0 {
		resolvedSubs = append(resolvedSubs, core.SubReconnectFrequent)
	}

	for _, h := range firingHits {
		st.active[h.subType] = true
	}
	for _, sub := range resolvedSubs {
		delete(st.active, sub)
	}

	res := Result{
		Object:   strings.TrimSpace(s.DeviceNo),
		DedupKey: core.Dedup(core.CodeDeviceHealth, core.HashID(s.DeviceNo)),
	}
	for sub := range st.active {
		res.Hits = append(res.Hits, sub)
	}
	// 先判断这次会不会真正发出，再扣自然日名额。扣早了会把名额用掉但告警被 10 分钟提醒间隔吃掉。
	outgoing := firingHits
	willSend := len(firingHits) > 0 && (!st.everFired || s.At.Sub(st.lastFireAt) >= core.DefaultRemind)
	if willSend {
		outgoing = e.keepWithinDaily(st, s.DeviceNo, s.At, firingHits)
		willSend = len(outgoing) > 0
	}
	if willSend && !st.everFired {
		st.everFired = true
		st.lastFireAt = s.At
		res.Fire = true
		res.FireText = joinHits(outgoing)
		return res
	}
	if willSend && st.everFired {
		st.lastFireAt = s.At
		res.Remind = true
		res.FireText = joinHits(outgoing)
		return res
	}
	if st.everFired && len(st.active) == 0 {
		st.everFired = false
		res.Resolve = true
		res.ResText = "设备心跳健康指标已经全部恢复正常"
	}
	return res
}

func (e *Engine) adjacentTimes(prev, now time.Time) bool {
	if prev.IsZero() || now.IsZero() {
		return false
	}
	return absDuration(now.Sub(prev)) <= e.th.PairWindow()
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func joinHits(hits []deviceHit) string {
	if len(hits) == 0 {
		return ""
	}
	out := hits[0].firingText
	for i := 1; i < len(hits); i++ {
		out += "；" + hits[i].firingText
	}
	return out
}
