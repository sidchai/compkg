package engine

import (
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

const (
	// instructionTimeout 监控口径 32s，与业务 cleanup 30s 刻意错开，不认 use_status=4。
	instructionTimeout   = 32 * time.Second
	instructionWindow    = 5 * time.Minute
	instructionMinCount  = 10
	instructionMinOnline = 3
)

// InstructionEvent 单条指令超时采样。监控不认 use_status=4，只认无成功响应。
type InstructionEvent struct {
	CmdType    string
	DeviceHash string
	Online     bool
	CreatedAt  time.Time
	SuccessAt  time.Time
}

// InstructionAggregator 5 分钟滚动窗口：超时>=10 且在线设备>=3 才 FIRING。
type InstructionAggregator struct {
	mu        sync.Mutex
	evs       []aggItem
	eval      *Evaluator
	minCount  int
	minOnline int
}

type aggItem struct {
	cmd       string
	device    string
	online    bool
	timeoutAt time.Time
	success   bool
}

// NewInstructionAggregator 单条超时或不足 3 台在线设备不得 FIRING。
func NewInstructionAggregator() *InstructionAggregator {
	return &InstructionAggregator{
		minCount:  instructionMinCount,
		minOnline: instructionMinOnline,
		eval: NewEvaluator(RuleSpec{
			FireConsecutive:    1,
			RecoverDuration:    instructionWindow,
			RecoverConsecutive: 0,
		}),
	}
}

// SetLimits 覆盖条数/在线台数阈值；≤0 保持原值。须在 Observe 前调用。
func (a *InstructionAggregator) SetLimits(minCount, minOnline int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if minCount > 0 {
		a.minCount = minCount
	}
	if minOnline > 0 {
		a.minOnline = minOnline
	}
}

func (a *InstructionAggregator) Observe(ev InstructionEvent, now time.Time) Action {
	a.mu.Lock()
	defer a.mu.Unlock()
	cut := now.Add(-instructionWindow)
	kept := a.evs[:0]
	for _, it := range a.evs {
		if it.timeoutAt.After(cut) {
			kept = append(kept, it)
		}
	}
	a.evs = kept

	if ev.SuccessAt.IsZero() && now.Sub(ev.CreatedAt) >= instructionTimeout {
		a.evs = append(a.evs, aggItem{
			cmd:       ev.CmdType,
			device:    ev.DeviceHash,
			online:    ev.Online,
			timeoutAt: now,
		})
	}

	count := 0
	online := map[string]struct{}{}
	for _, it := range a.evs {
		if ev.CmdType != "" && it.cmd != ev.CmdType {
			continue
		}
		count++
		if it.online {
			online[it.device] = struct{}{}
		}
	}
	abnormal := count >= a.minCount && len(online) >= a.minOnline
	key := core.Dedup(core.CodeInstructionTimeout, ev.CmdType)
	return a.eval.Observe(key, abnormal, now)
}

func (a *InstructionAggregator) Counts(cmdType string, now time.Time) (timeouts, onlineDevices int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cut := now.Add(-instructionWindow)
	online := map[string]struct{}{}
	for _, it := range a.evs {
		if !it.timeoutAt.After(cut) {
			continue
		}
		if cmdType != "" && it.cmd != cmdType {
			continue
		}
		timeouts++
		if it.online {
			online[it.device] = struct{}{}
		}
	}
	return timeouts, len(online)
}
