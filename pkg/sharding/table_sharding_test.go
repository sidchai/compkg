package sharding

import (
	"testing"
	"time"
)

func TestWriteTableCutover(t *testing.T) {
	cfg, err := normalizeTableShardingConfig(TableShardingConfig{
		Mode:              ShardModeHash,
		TableName:         "api_request_record",
		TablePrefix:       "api_request_record_",
		AccountShardCount: 64,
		CutoverAt:         1700000000,
	}, "api_request_record", "api_request_record_")
	if err != nil {
		t.Fatal(err)
	}

	legacy, err := cfg.writeTable(100, "D1", time.Unix(1699999999, 0))
	if err != nil || legacy != "api_request_record" {
		t.Fatalf("before cutover: got %s err=%v", legacy, err)
	}

	_, err = cfg.writeTable(0, "D1", time.Unix(1700000001, 0))
	if err != ErrInvalidAccountID {
		t.Fatalf("expect ErrInvalidAccountID, got %v", err)
	}

	shard, err := cfg.writeTable(100, "D1", time.Unix(1700000001, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := cfg.shardPhysicalTable(100, "D1")
	if shard != want || shard != "api_request_record_36" { // 100%64=36
		t.Fatalf("shard=%s want=%s", shard, want)
	}
}

func TestReadPlanByCutover(t *testing.T) {
	cfg, err := normalizeTableShardingConfig(TableShardingConfig{
		Mode:              ShardModeHash,
		TableName:         "api_request_record",
		TablePrefix:       "api_request_record_",
		AccountShardCount: 64,
		CutoverAt:         1700000000,
	}, "api_request_record", "api_request_record_")
	if err != nil {
		t.Fatal(err)
	}

	p1 := cfg.resolveReadPlan([]int64{100}, "", 1690000000, 1699999999)
	if p1.Source != ReadLegacy {
		t.Fatalf("expect legacy, got %v", p1.Source)
	}

	p2 := cfg.resolveReadPlan([]int64{100}, "", 1700000000, 1700001000)
	if p2.Source != ReadShard || len(p2.ShardTables) != 1 {
		t.Fatalf("expect shard, got %+v", p2)
	}

	p3 := cfg.resolveReadPlan([]int64{100}, "", 1699990000, 1700001000)
	if p3.Source != ReadBoth || p3.LegacyCreatedAtLt != 1700000000 || p3.ShardCreatedAtGte != 1700000000 {
		t.Fatalf("expect both, got %+v", p3)
	}
}

func TestSingleModeAlwaysLegacy(t *testing.T) {
	cfg, err := normalizeTableShardingConfig(TableShardingConfig{
		Mode:      ShardModeSingle,
		TableName: "device_log",
		CutoverAt: 1700000000,
	}, "device_log", "device_log_")
	if err != nil {
		t.Fatal(err)
	}
	name, err := cfg.writeTable(1, "D", time.Unix(1800000000, 0))
	if err != nil || name != "device_log" {
		t.Fatalf("got %s err=%v", name, err)
	}
	if len(cfg.allTablesForCleanup()) != 1 {
		t.Fatalf("cleanup should only legacy")
	}
}

func TestHotspotDeviceSubShard(t *testing.T) {
	cfg, err := normalizeTableShardingConfig(TableShardingConfig{
		Mode:                    ShardModeHash,
		TableName:               "device_log",
		TablePrefix:             "device_log_",
		AccountShardCount:       64,
		HotspotAccounts:         []int64{100},
		HotspotDeviceShardCount: 8,
	}, "device_log", "device_log_")
	if err != nil {
		t.Fatal(err)
	}
	name := cfg.shardPhysicalTable(100, "ABC")
	if name == "device_log_36" {
		t.Fatalf("hotspot should use sub shard, got %s", name)
	}
	normal := cfg.shardPhysicalTable(101, "ABC")
	if normal != "device_log_37" { // 101%64=37
		t.Fatalf("normal account got %s", normal)
	}
}
