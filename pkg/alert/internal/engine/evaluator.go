// Package engine 实现规则状态机、根因抑制和指令群体聚合。
package engine

import (
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// State 监控对象状态机：NORMAL → PENDING → FIRING → RESOLVED → NORMAL。
type State string

const (
	StateNormal   State = "NORMAL"   // 未达条件或已恢复
	StatePending  State = "PENDING"  // 已异常但未满连续次数/时长
	StateFiring   State = "FIRING"   // 已推送首告，持续异常只提醒
	StateResolved State = "RESOLVED" // 瞬时态，发出恢复后立刻回 NORMAL
)

// Action 状态机输出。未 FIRING 过不得产生 Resolve。
type Action int

const (
	ActionNone Action = iota
	ActionFire
	ActionRemind
	ActionResolve
)

// RuleSpec 描述连续次数或持续时间。只填需要的一侧。
type RuleSpec struct {
	FireConsecutive    int
	FireDuration       time.Duration
	RecoverConsecutive int
	RecoverDuration    time.Duration
	RemindInterval     time.Duration
}

func (s RuleSpec) remind() time.Duration {
	if s.RemindInterval <= 0 {
		return core.DefaultRemind
	}
	return s.RemindInterval
}

type objectState struct {
	state          State
	abnormalStreak int
	normalStreak   int
	abnormalSince  time.Time
	normalSince    time.Time
	lastFireAt     time.Time
	everFired      bool
}

// Evaluator 按 dedupKey 维护状态。并发安全。
type Evaluator struct {
	spec RuleSpec
	mu   sync.Mutex
	m    map[string]*objectState
}

// NewEvaluator 两侧阈值都未填时按“一次即触发/一次即恢复”，避免空规则空转。
func NewEvaluator(spec RuleSpec) *Evaluator {
	if spec.FireConsecutive <= 0 && spec.FireDuration <= 0 {
		spec.FireConsecutive = 1
	}
	if spec.RecoverConsecutive <= 0 && spec.RecoverDuration <= 0 {
		spec.RecoverConsecutive = 1
	}
	return &Evaluator{spec: spec, m: make(map[string]*objectState)}
}

// Observe 摄入一次采样。abnormal 中途变 false 会清零连续计数，防止抖动误报。
func (e *Evaluator) Observe(dedupKey string, abnormal bool, now time.Time) Action {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.m[dedupKey]
	if st == nil {
		st = &objectState{state: StateNormal}
		e.m[dedupKey] = st
	}
	if abnormal {
		return e.onAbnormal(st, now)
	}
	return e.onNormal(st, now)
}

func (e *Evaluator) onAbnormal(st *objectState, now time.Time) Action {
	st.normalStreak = 0
	st.normalSince = time.Time{}
	st.abnormalStreak++
	if st.abnormalSince.IsZero() {
		st.abnormalSince = now
	}
	if st.state == StateFiring {
		if now.Sub(st.lastFireAt) >= e.spec.remind() {
			st.lastFireAt = now
			return ActionRemind
		}
		return ActionNone
	}
	if !e.fireReached(st, now) {
		st.state = StatePending
		return ActionNone
	}
	st.state = StateFiring
	st.everFired = true
	st.lastFireAt = now
	return ActionFire
}

func (e *Evaluator) onNormal(st *objectState, now time.Time) Action {
	st.abnormalStreak = 0
	st.abnormalSince = time.Time{}
	st.normalStreak++
	if st.normalSince.IsZero() {
		st.normalSince = now
	}
	if st.state != StateFiring {
		st.state = StateNormal
		st.everFired = false
		st.lastFireAt = time.Time{}
		return ActionNone
	}
	if !e.recoverReached(st, now) {
		return ActionNone
	}
	st.state = StateResolved
	action := ActionNone
	if st.everFired {
		action = ActionResolve
	}
	st.everFired = false
	st.lastFireAt = time.Time{}
	st.state = StateNormal
	return action
}

func (e *Evaluator) fireReached(st *objectState, now time.Time) bool {
	okConsec := e.spec.FireConsecutive <= 0 || st.abnormalStreak >= e.spec.FireConsecutive
	okDur := e.spec.FireDuration <= 0 || now.Sub(st.abnormalSince) >= e.spec.FireDuration
	return okConsec && okDur
}

func (e *Evaluator) recoverReached(st *objectState, now time.Time) bool {
	okConsec := e.spec.RecoverConsecutive <= 0 || st.normalStreak >= e.spec.RecoverConsecutive
	okDur := e.spec.RecoverDuration <= 0 || now.Sub(st.normalSince) >= e.spec.RecoverDuration
	return okConsec && okDur
}

// StateOf 供单测观察中间态；业务告警只认 Action，不读 State。
func (e *Evaluator) StateOf(dedupKey string) State {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.m[dedupKey]
	if st == nil {
		return StateNormal
	}
	return st.state
}

// Forget 删除状态，给 TTL 回收用，避免 10 万设备对象常驻。
func (e *Evaluator) Forget(dedupKey string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.m, dedupKey)
}
