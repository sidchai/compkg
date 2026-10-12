package core

import "testing"

func TestP0ThresholdsApplyDefaults(t *testing.T) {
	var p P0Thresholds
	p.ApplyDefaults()
	if p.WaitUploadGT != 200 || p.TempGT != 65 || p.MQProducerPendingSec != 30 || p.DeviceDailyFireLimit != 1 {
		t.Fatalf("defaults=%+v", p)
	}
	p.WaitUploadGT = 80
	p.ApplyDefaults()
	if p.WaitUploadGT != 80 {
		t.Fatal("explicit value must stay")
	}
}
