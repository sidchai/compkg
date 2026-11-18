package sharding

import (
	"sync"
	"time"
)

const (
	DeviceLogShardModeSingle = ShardModeSingle
	DeviceLogShardModeHash   = ShardModeHash

	DeviceLogShardCount = DefaultAccountShardCount

	defaultDeviceLogTableName   = "device_log"
	defaultDeviceLogTablePrefix = "device_log_"
)

// DeviceLogShardingConfig 与 api_request_record 同结构。
type DeviceLogShardingConfig = TableShardingConfig

var (
	deviceLogShardingMu sync.RWMutex
	deviceLogSharding   = DefaultDeviceLogShardingConfig()
)

func DefaultDeviceLogShardingConfig() DeviceLogShardingConfig {
	return DeviceLogShardingConfig{
		Mode:                    DeviceLogShardModeHash,
		TableName:               defaultDeviceLogTableName,
		TablePrefix:             defaultDeviceLogTablePrefix,
		AccountShardCount:       DeviceLogShardCount,
		CutoverAt:               0,
		DeviceShardCount:        0,
		HotspotDeviceShardCount: DefaultHotspotDeviceShardCount,
		PhysicalShardCount:      DeviceLogShardCount,
	}
}

func ConfigureDeviceLogSharding(cfg DeviceLogShardingConfig) error {
	normalized, err := normalizeTableShardingConfig(cfg, defaultDeviceLogTableName, defaultDeviceLogTablePrefix)
	if err != nil {
		return err
	}
	deviceLogShardingMu.Lock()
	deviceLogSharding = normalized
	deviceLogShardingMu.Unlock()
	return nil
}

func deviceLogCfg() TableShardingConfig {
	deviceLogShardingMu.RLock()
	cfg := deviceLogSharding
	deviceLogShardingMu.RUnlock()
	return cfg
}

func DeviceLogCutoverAt() int64 {
	return deviceLogCfg().CutoverAt
}

func DeviceLogIsCutover() bool {
	return deviceLogCfg().isCutoverActive(time.Now())
}

func DeviceLogLegacyTable() string {
	return deviceLogCfg().TableName
}

func DeviceLogWriteTable(accountID int64, deviceNo string) (string, error) {
	return deviceLogCfg().writeTable(accountID, deviceNo, time.Now())
}

// DeviceLogWriteTableAt 按业务时间 at 相对 T_cut 选写入表（迁移保留 created_at 时用）。
func DeviceLogWriteTableAt(accountID int64, deviceNo string, at time.Time) (string, error) {
	return deviceLogCfg().writeTable(accountID, deviceNo, at)
}

func DeviceLogShardTable(accountID int64, deviceNo string) string {
	return deviceLogCfg().shardPhysicalTable(accountID, deviceNo)
}

func DeviceLogReadPlan(accountIDs []int64, deviceNo string, tStart, tEnd int64) ReadPlan {
	return deviceLogCfg().resolveReadPlan(accountIDs, deviceNo, tStart, tEnd)
}

func DeviceLogLookupTables(accountID int64, deviceNo string) []string {
	return deviceLogCfg().lookupTables(accountID, deviceNo, time.Now())
}

func AllDeviceLogTables() []string {
	return deviceLogCfg().allTablesForCleanup()
}

func AllDeviceLogShardTables() []string {
	return deviceLogCfg().allShardTables()
}
