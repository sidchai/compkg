package engine

import (
	"testing"
	"time"
)

func TestEvaluator_PendingFireRemindResolve(t *testing.T) {
	ev := NewEvaluator(RuleSpec{FireConsecutive: 3, RecoverConsecutive: 2, RemindInterval: 10 * time.Minute})
	now := time.Unix(1_700_000_000, 0)
	key := "k1"

	if got := ev.Observe(key, true, now); got != ActionNone {
		t.Fatalf("1st abnormal want none, got %v", got)
	}
	if ev.StateOf(key) != StatePending {
		t.Fatalf("want PENDING")
	}
	if got := ev.Observe(key, true, now.Add(time.Second)); got != ActionNone {
		t.Fatalf("2nd want none")
	}
	if got := ev.Observe(key, true, now.Add(2*time.Second)); got != ActionFire {
		t.Fatalf("3rd want fire, got %v", got)
	}
	if ev.StateOf(key) != StateFiring {
		t.Fatalf("want FIRING")
	}
	if got := ev.Observe(key, true, now.Add(3*time.Second)); got != ActionNone {
		t.Fatalf("sustain should not re-fire immediately")
	}
	if got := ev.Observe(key, true, now.Add(12*time.Minute)); got != ActionRemind {
		t.Fatalf("want remind after 10m")
	}
	if got := ev.Observe(key, false, now.Add(13*time.Minute)); got != ActionNone {
		t.Fatalf("first recover sample still firing")
	}
	if got := ev.Observe(key, false, now.Add(14*time.Minute)); got != ActionResolve {
		t.Fatalf("want resolve")
	}
	if ev.StateOf(key) != StateNormal {
		t.Fatalf("after resolve want NORMAL")
	}
}

func TestEvaluator_NoResolveWithoutFire(t *testing.T) {
	ev := NewEvaluator(RuleSpec{FireConsecutive: 3, RecoverConsecutive: 1})
	now := time.Now()
	ev.Observe("k", true, now)
	ev.Observe("k", true, now.Add(time.Second))
	if got := ev.Observe("k", false, now.Add(2*time.Second)); got != ActionNone {
		t.Fatalf("never fired must not resolve, got %v", got)
	}
}

func TestEvaluator_DurationBoundary(t *testing.T) {
	ev := NewEvaluator(RuleSpec{FireDuration: 5 * time.Minute, RecoverDuration: 5 * time.Minute})
	now := time.Unix(1_800_000_000, 0)
	if ev.Observe("d", true, now) != ActionNone {
		t.Fatal("just start")
	}
	if ev.Observe("d", true, now.Add(5*time.Minute-time.Second)) != ActionNone {
		t.Fatal("1ms before 5m")
	}
	if ev.Observe("d", true, now.Add(5*time.Minute)) != ActionFire {
		t.Fatal("exactly 5m should fire")
	}
	if ev.Observe("d", false, now.Add(6*time.Minute)) != ActionNone {
		t.Fatal("recover not yet")
	}
	if ev.Observe("d", false, now.Add(11*time.Minute)) != ActionResolve {
		t.Fatal("exactly +5m recover")
	}
}

func TestEvaluator_MidNormalResetsStreak(t *testing.T) {
	ev := NewEvaluator(RuleSpec{FireConsecutive: 3})
	now := time.Now()
	ev.Observe("k", true, now)
	ev.Observe("k", true, now.Add(time.Second))
	ev.Observe("k", false, now.Add(2*time.Second))
	if ev.Observe("k", true, now.Add(3*time.Second)) != ActionNone {
		t.Fatal("streak should reset")
	}
}

func TestInstructionAggregatorThreshold(t *testing.T) {
	a := NewInstructionAggregator()
	now := time.Unix(3_000_000_000, 0)
	for i := 0; i < 9; i++ {
		dev := "d" + string(rune('a'+i%3))
		act := a.Observe(InstructionEvent{CmdType: "reboot", DeviceHash: dev, Online: true, CreatedAt: now.Add(-33 * time.Second)}, now)
		if act == ActionFire {
			t.Fatal("9 should not fire")
		}
	}
	act := a.Observe(InstructionEvent{CmdType: "reboot", DeviceHash: "dx", Online: true, CreatedAt: now.Add(-33 * time.Second)}, now)
	if act != ActionFire {
		t.Fatalf("10/3 should fire, got %v", act)
	}
}

func TestInstructionAggregatorOnlineOverride(t *testing.T) {
	a := NewInstructionAggregator()
	a.SetLimits(10, 50)
	now := time.Unix(3_100_000_000, 0)
	for i := 0; i < 20; i++ {
		dev := "d" + string(rune('A'+i%10))
		act := a.Observe(InstructionEvent{CmdType: "reboot", DeviceHash: dev, Online: true, CreatedAt: now.Add(-33 * time.Second)}, now)
		if act == ActionFire {
			t.Fatal("10 online devices must not fire when minOnline=50")
		}
	}
}

func TestInstructionSingleDeviceNoFire(t *testing.T) {
	a := NewInstructionAggregator()
	now := time.Now()
	for i := 0; i < 12; i++ {
		act := a.Observe(InstructionEvent{CmdType: "reboot", DeviceHash: "only", Online: true, CreatedAt: now.Add(-33 * time.Second)}, now)
		if act == ActionFire {
			t.Fatal("single device must not fire")
		}
	}
}

func TestSuppressOpenAPIWhenServiceDown(t *testing.T) {
	if MergeOpenAPIIntoService(true) {
		t.Fatal("should suppress openapi")
	}
	s := NewSuppressor()
	now := time.Now()
	if !s.ShouldSend("host-1", "storage.critical", now) {
		t.Fatal("first storage")
	}
	if s.ShouldSend("host-1", "mq.flow_abnormal", now) {
		t.Fatal("mq flow should be suppressed by storage")
	}
}
