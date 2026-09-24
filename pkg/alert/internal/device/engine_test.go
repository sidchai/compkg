package device

import (
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
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

	wait := Sample{DeviceNo: "CONS0003XXXX", HasWait: true, WaitUpload: 120, At: now}
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
