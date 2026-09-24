package core

// HostSnapshot 本机资源快照。仅 iot_server 与 platform_server 采集。
type HostSnapshot struct {
	CPUMinuteAvg      float64 // 分钟均值，禁止用瞬时峰值
	MemAvailableRatio float64 // MemAvailable/总量，禁止用 free 替代
	Disks             []DiskSample
	OOMOccurred       bool // 新 OOM 或关键进程被 killer 终止
}

// DiskSample 一个关键挂载点。
type DiskSample struct {
	Mount      string
	FreeGB     float64
	ReadOnly   bool
	WriteFails int
}

// HostCollector 主机采集接口，实现放在采集服务内或 hostlinux。
type HostCollector interface {
	Collect() (HostSnapshot, error)
}

// CPUAbnormal 单点判定：>90 异常，<80 可恢复，80～90 保持当前状态以免抖动。
func CPUAbnormal(avg float64) (abnormal, recover bool, hold bool) {
	if avg > 90 {
		return true, false, false
	}
	if avg < 80 {
		return false, true, false
	}
	return false, false, true
}

// MemAbnormal MemAvailable 占比 <15% 为异常；>=20% 可恢复。
func MemAbnormal(ratio float64) (abnormal, recover bool) {
	return ratio < 0.15, ratio >= 0.20
}

// DiskAbnormal 剩余 <5GB 或只读。
func DiskAbnormal(d DiskSample) bool {
	return d.FreeGB < 5 || d.ReadOnly
}

// DiskRecover 剩余 >=10GB 且可写。
func DiskRecover(d DiskSample) bool {
	return d.FreeGB >= 10 && !d.ReadOnly
}
