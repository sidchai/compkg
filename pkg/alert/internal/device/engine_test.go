package device

import (
	"strings"
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
	"github.com/sidchai/compkg/pkg/alert/internal/store"
)

func TestDeviceTempConsecutiveAndIsolate(t *testing.T) {
	e := New(100, nil)
	now := time.Unix(1_900_000_000, 0)
	a := Sample{DeviceNo: "DEV0001AAAA", HasTemp: true, Temperature: 70, At: now}
	b := Sample{DeviceNo: "DEV0002BBBB", HasTemp: true, Temperature: 20, At: now}

	if e.Evaluate(a).Fire {
		t.Fatal("first high temp should not fire")
	}
	a.At = now.Add(time.Minute)
	res := e.Evaluate(a)
	if !res.Fire {
		t.Fatal("second high temp should fire")
	}
	if res.Object != "DEV0001AAAA" {
		t.Fatalf("device no must be plaintext, got %s", res.Object)
	}
	if e.Evaluate(b).Fire {
		t.Fatal("other device must isolate")
	}
	a.Temperature = 50
	a.At = now.Add(2 * time.Minute)
	if e.Evaluate(a).Resolve {
		t.Fatal("need two recover samples")
	}
	a.At = now.Add(3 * time.Minute)
	if !e.Evaluate(a).Resolve {
		t.Fatal("want resolve")
	}
}

func TestDeviceConsecutiveNeed6Min(t *testing.T) {
	e := New(10, nil)
	now := time.Unix(2_400_000_000, 0)
	temp := Sample{DeviceNo: "CONS0001XXXX", HasTemp: true, Temperature: 70, At: now}
	e.Evaluate(temp)
	temp.At = now.Add(6*time.Minute + time.Second)
	if e.Evaluate(temp).Fire {
		t.Fatal("two high temps more than 6min apart must not fire")
	}
	temp.At = now.Add(8 * time.Minute)
	if !e.Evaluate(temp).Fire {
		t.Fatal("two high temps within 6min should fire")
	}

	sd := Sample{DeviceNo: "CONS0002XXXX", HasSD: true, SDRemainMB: 10, At: now}
	e.Evaluate(sd)
	sd.At = now.Add(7 * time.Minute)
	if e.Evaluate(sd).Fire {
		t.Fatal("two low SD more than 6min apart must not fire")
	}

	wait := Sample{DeviceNo: "CONS0003XXXX", HasWait: true, WaitUpload: 201, At: now}
	e.Evaluate(wait)
	wait.At = now.Add(7 * time.Minute)
	if e.Evaluate(wait).Fire {
		t.Fatal("two backlog samples more than 6min apart must not fire")
	}
}

func TestDeviceReconnectWindow(t *testing.T) {
	e := New(10, nil)
	now := time.Unix(2_000_000_000, 0)
	dev := "RECONN0001XXXX"
	s := Sample{DeviceNo: dev, HasOnline: true, Online: false, At: now}
	e.Evaluate(s)
	for i := 1; i <= 5; i++ {
		s.Online = true
		s.At = now.Add(time.Duration(i) * time.Minute)
		r := e.Evaluate(s)
		s.Online = false
		s.At = s.At.Add(time.Second)
		e.Evaluate(s)
		if i < 5 && r.Fire {
			t.Fatalf("fire too early at %d", i)
		}
		if i == 5 && !r.Fire {
			t.Fatal("5 reconnects should fire")
		}
	}
}

func TestDeviceExplicitReconnect(t *testing.T) {
	e := New(10, nil)
	now := time.Unix(2_100_000_000, 0)
	dev := "RECONN0002XXXX"
	var r Result
	for i := 0; i < 5; i++ {
		r = e.Evaluate(Sample{DeviceNo: dev, Reconnect: true, At: now.Add(time.Duration(i) * time.Minute)})
	}
	if !r.Fire {
		t.Fatal("5 explicit reconnects should fire")
	}
}

func TestDeviceTTLAndCapacity(t *testing.T) {
	e := New(2, nil)
	e.ttl = 10 * time.Millisecond
	now := time.Now()
	e.Evaluate(Sample{DeviceNo: "A1", HasTemp: true, Temperature: 1, At: now})
	e.Evaluate(Sample{DeviceNo: "B1", HasTemp: true, Temperature: 1, At: now})
	if e.Size() != 2 {
		t.Fatalf("size=%d", e.Size())
	}
	if e.Evaluate(Sample{DeviceNo: "C1", HasTemp: true, Temperature: 1, At: now}).Fire {
		t.Fatal("over cap")
	}
	if e.getOrCreate("C1", now) != nil {
		t.Fatal("third device must be rejected")
	}
	time.Sleep(20 * time.Millisecond)
	sh := e.shardOf("A1")
	sh.mu.Lock()
	e.gcShard(sh, time.Now())
	sh.mu.Unlock()
	if e.Size() > 2 {
		t.Fatal("ttl should reclaim")
	}
}

func TestDeviceBatteryJump(t *testing.T) {
	e := New(10, nil)
	now := time.Unix(2_200_000_000, 0)
	dev := "BATT0001XXXX"
	s := Sample{DeviceNo: dev, HasBattery: true, Battery: 20, At: now}
	if e.Evaluate(s).Fire {
		t.Fatal("first battery sample is seed only")
	}
	s.Battery = 40
	s.At = now.Add(time.Minute)
	if e.Evaluate(s).Fire {
		t.Fatal("20% to 40% is 20 points, must not fire")
	}
	s.Battery = 80
	s.At = now.Add(2 * time.Minute)
	r := e.Evaluate(s)
	if !r.Fire {
		t.Fatal("40% to 80% exceeds 30 points, should fire")
	}
	if r.FireText != "设备上报的电量变化超过30%，从40%变为80%" {
		t.Fatalf("phenomenon=%q", r.FireText)
	}
}

func TestDeviceBatteryJumpAdjacentAtExactly6Min(t *testing.T) {
	e := New(10, nil)
	now := time.Unix(2_250_000_000, 0)
	s := Sample{DeviceNo: "BATT0003XXXX", HasBattery: true, Battery: 20, At: now}
	e.Evaluate(s)
	s.Battery = 80
	s.At = now.Add(6 * time.Minute)
	if !e.Evaluate(s).Fire {
		t.Fatal("gap exactly 6min is adjacent, 20% to 80% should fire")
	}
}

func TestDeviceBatteryJumpNotAdjacentOver6Min(t *testing.T) {
	e := New(10, nil)
	now := time.Unix(2_300_000_000, 0)
	dev := "BATT0002XXXX"
	s := Sample{DeviceNo: dev, HasBattery: true, Battery: 20, At: now}
	e.Evaluate(s)
	s.Battery = 80
	s.At = now.Add(6*time.Minute + time.Second)
	if e.Evaluate(s).Fire {
		t.Fatal("device time gap >6min is not adjacent, must not fire jump")
	}
	// 上一笔已把 80% 记为基准；间隔约 2 分钟算相邻，80%→20% 应告。
	s.Battery = 20
	s.At = now.Add(8 * time.Minute)
	if !e.Evaluate(s).Fire {
		t.Fatal("80% to 20% within 6min should fire")
	}
}

func TestDeviceWaitUploadOverride(t *testing.T) {
	e := New(10, nil)
	e.SetThresholds(core.P0Thresholds{WaitUploadGT: 50})
	now := time.Unix(2_500_000_000, 0)
	s := Sample{DeviceNo: "WAIT0001XXXX", HasWait: true, WaitUpload: 51, At: now}
	if e.Evaluate(s).Fire {
		t.Fatal("first backlog sample must not fire")
	}
	s.At = now.Add(time.Minute)
	r := e.Evaluate(s)
	if !r.Fire {
		t.Fatal("yaml wait_upload_gt=50 should fire at 51")
	}
	if r.FireText != "设备连续2次上报待上传录音超过50条，当前51条" {
		t.Fatalf("phenomenon=%q", r.FireText)
	}
}

func TestDeviceSDAndWaitOncePerNaturalDay(t *testing.T) {
	e := New(10, nil)
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, loc)
	sd := Sample{DeviceNo: "SDDAY0001XXXX", HasSD: true, SDRemainMB: 100, At: now}
	if e.Evaluate(sd).Fire {
		t.Fatal("first low sd must not fire")
	}
	sd.At = now.Add(time.Minute)
	if !e.Evaluate(sd).Fire {
		t.Fatal("second low sd should fire")
	}
	for i := 2; i <= 12; i++ {
		sd.At = now.Add(time.Duration(i) * time.Minute)
		r := e.Evaluate(sd)
		if r.Fire || r.Remind {
			t.Fatalf("same day sd must not fire again at +%dm", i)
		}
	}
	sd.SDRemainMB = 500
	sd.At = now.Add(13 * time.Minute)
	e.Evaluate(sd)
	sd.At = now.Add(14 * time.Minute)
	if !e.Evaluate(sd).Resolve {
		t.Fatal("two recovered sd samples should resolve")
	}
	sd.SDRemainMB = 100
	sd.At = now.Add(15 * time.Minute)
	e.Evaluate(sd)
	sd.At = now.Add(16 * time.Minute)
	if e.Evaluate(sd).Fire || e.Evaluate(sd).Remind {
		t.Fatal("sd re-breach same natural day must not fire")
	}

	wait := Sample{DeviceNo: "SDDAY0001XXXX", HasWait: true, WaitUpload: 201, At: now.Add(17 * time.Minute)}
	e.Evaluate(wait)
	wait.At = now.Add(18 * time.Minute)
	if !e.Evaluate(wait).Fire {
		t.Fatal("wait backlog is a separate daily quota")
	}
	wait.At = now.Add(30 * time.Minute)
	if e.Evaluate(wait).Fire || e.Evaluate(wait).Remind {
		t.Fatal("wait backlog same day must not remind")
	}

	next := time.Date(2026, 10, 13, 0, 5, 0, 0, loc)
	sd.SDRemainMB = 100
	sd.At = next
	e.Evaluate(sd)
	sd.At = next.Add(time.Minute)
	r := e.Evaluate(sd)
	if !r.Fire && !r.Remind {
		t.Fatal("next natural day should trigger again")
	}
	if r.FireText == "" || !strings.Contains(r.FireText, "SD卡剩余空间不足150MB") {
		t.Fatalf("next day phenomenon=%q", r.FireText)
	}
}

func TestDeviceDailyFireLimitTwo(t *testing.T) {
	e := New(10, nil)
	e.SetThresholds(core.P0Thresholds{DeviceDailyFireLimit: 2, SDRemainMBLT: 150})
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	sd := Sample{DeviceNo: "SDDAY0002XXXX", HasSD: true, SDRemainMB: 10, At: now}
	e.Evaluate(sd)
	sd.At = now.Add(time.Minute)
	if !e.Evaluate(sd).Fire {
		t.Fatal("first incident should fire")
	}
	sd.SDRemainMB = 400
	sd.At = now.Add(2 * time.Minute)
	e.Evaluate(sd)
	sd.At = now.Add(3 * time.Minute)
	if !e.Evaluate(sd).Resolve {
		t.Fatal("want resolve before second incident")
	}
	sd.SDRemainMB = 10
	sd.At = now.Add(4 * time.Minute)
	e.Evaluate(sd)
	sd.At = now.Add(5 * time.Minute)
	if !e.Evaluate(sd).Fire {
		t.Fatal("limit 2 should allow the second incident")
	}
	sd.SDRemainMB = 400
	sd.At = now.Add(6 * time.Minute)
	e.Evaluate(sd)
	sd.At = now.Add(7 * time.Minute)
	e.Evaluate(sd)
	sd.SDRemainMB = 10
	sd.At = now.Add(8 * time.Minute)
	e.Evaluate(sd)
	sd.At = now.Add(9 * time.Minute)
	if e.Evaluate(sd).Fire || e.Evaluate(sd).Remind {
		t.Fatal("third incident same day must stay quiet")
	}
}

func TestDeviceWaitUploadDefaultIs200(t *testing.T) {
	e := New(10, nil)
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	s := Sample{DeviceNo: "WAIT0200XXXX", HasWait: true, WaitUpload: 200, At: now}
	e.Evaluate(s)
	s.At = now.Add(time.Minute)
	if e.Evaluate(s).Fire {
		t.Fatal("200 is not greater than 200")
	}
	s.WaitUpload = 201
	s.At = now.Add(2 * time.Minute)
	e.Evaluate(s)
	s.At = now.Add(3 * time.Minute)
	r := e.Evaluate(s)
	if !r.Fire {
		t.Fatal("201 twice should fire at default 200")
	}
	if r.FireText != "设备连续2次上报待上传录音超过200条，当前201条" {
		t.Fatalf("phenomenon=%q", r.FireText)
	}
}

func TestDeviceTempRemindNotDailyLimited(t *testing.T) {
	e := New(10, nil)
	now := time.Date(2026, 10, 12, 11, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	s := Sample{DeviceNo: "TEMPREM0001XX", HasTemp: true, Temperature: 80, At: now}
	e.Evaluate(s)
	s.At = now.Add(time.Minute)
	if !e.Evaluate(s).Fire {
		t.Fatal("temp should fire")
	}
	reminded := false
	for i := 2; i <= 12; i++ {
		s.At = now.Add(time.Duration(i) * time.Minute)
		if e.Evaluate(s).Remind {
			reminded = true
		}
	}
	if !reminded {
		t.Fatal("temperature remind must stay on the 10 minute cadence")
	}
}

func TestDeviceDailySharedStore(t *testing.T) {
	st := store.NewMemoryStore(10)
	a := New(10, nil)
	b := New(10, nil)
	a.SetStore(st)
	b.SetStore(st)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	s := Sample{DeviceNo: "SDSHARE0001XX", HasSD: true, SDRemainMB: 10, At: now}
	a.Evaluate(s)
	s.At = now.Add(time.Minute)
	if !a.Evaluate(s).Fire {
		t.Fatal("first instance should fire")
	}
	other := Sample{DeviceNo: "SDSHARE0001XX", HasSD: true, SDRemainMB: 10, At: now.Add(2 * time.Minute)}
	b.Evaluate(other)
	other.At = now.Add(3 * time.Minute)
	if r := b.Evaluate(other); r.Fire || r.Remind {
		t.Fatal("second instance same device same day must not fire")
	}
}

func TestDeviceExclude(t *testing.T) {
	e := New(10, []string{"TESTDEV"})
	now := time.Now()
	s := Sample{DeviceNo: "TESTDEV", HasTemp: true, Temperature: 90, At: now}
	e.Evaluate(s)
	s.At = now.Add(time.Minute)
	if e.Evaluate(s).Fire {
		t.Fatal("excluded device")
	}
}
