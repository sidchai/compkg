package sharding

import (
	"sync"
	"time"
)

const (
	ApiRequestRecordShardModeSingle = ShardModeSingle
	ApiRequestRecordShardModeHash   = ShardModeHash

	// ApiRequestRecordShardCount 账户分片数默认值（原 PhysicalShardCount 语义已切换为账户维）。
	ApiRequestRecordShardCount = DefaultAccountShardCount
	// ApiRequestRecordVirtualShardCount 保留兼容字段，路由不再使用。
	ApiRequestRecordVirtualShardCount = 1024

	defaultApiRequestRecordTableName   = "api_request_record"
	defaultApiRequestRecordTablePrefix = "api_request_record_"
)

// ApiRequestRecordShardingConfig 对外配置（字段对齐方案 YAML）。
type ApiRequestRecordShardingConfig = TableShardingConfig

var (
	apiRequestRecordShardingMu sync.RWMutex
	apiRequestRecordSharding   = DefaultApiRequestRecordShardingConfig()
)

func DefaultApiRequestRecordShardingConfig() ApiRequestRecordShardingConfig {
	return ApiRequestRecordShardingConfig{
		Mode:               ApiRequestRecordShardModeHash,
		TableName:          defaultApiRequestRecordTableName,
		TablePrefix:        defaultApiRequestRecordTablePrefix,
		AccountShardCount:       ApiRequestRecordShardCount,
		CutoverAt:               0,
		DeviceShardCount:        0,
		HotspotDeviceShardCount: DefaultHotspotDeviceShardCount,
		PhysicalShardCount: ApiRequestRecordShardCount,
		VirtualShardCount:  ApiRequestRecordVirtualShardCount,
	}
}

func ConfigureApiRequestRecordSharding(cfg ApiRequestRecordShardingConfig) error {
	normalized, err := normalizeTableShardingConfig(cfg, defaultApiRequestRecordTableName, defaultApiRequestRecordTablePrefix)
	if err != nil {
		return err
	}
	apiRequestRecordShardingMu.Lock()
	apiRequestRecordSharding = normalized
	apiRequestRecordShardingMu.Unlock()
	return nil
}

func apiRequestRecordCfg() TableShardingConfig {
	apiRequestRecordShardingMu.RLock()
	cfg := apiRequestRecordSharding
	apiRequestRecordShardingMu.RUnlock()
	return cfg
	}

// ApiRequestRecordCutoverAt 返回 T_cut（unix 秒）；0 表示未切流。
func ApiRequestRecordCutoverAt() int64 {
	return apiRequestRecordCfg().CutoverAt
	}

// ApiRequestRecordIsCutover 当前是否已切到分片写。
func ApiRequestRecordIsCutover() bool {
	return apiRequestRecordCfg().isCutoverActive(time.Now())
	}

// ApiRequestRecordLegacyTable 旧单表名。
func ApiRequestRecordLegacyTable() string {
	return apiRequestRecordCfg().TableName
	}

// ApiRequestRecordWriteTable 写入路由：未切流→旧单表；已切流→账户分片（account_id 必填）。
func ApiRequestRecordWriteTable(accountID int64, deviceNo string) (string, error) {
	return apiRequestRecordCfg().writeTable(accountID, deviceNo, time.Now())
	}

// ApiRequestRecordShardTable 仅计算账户分片物理表（忽略 cutover，用于建表/对账）。
func ApiRequestRecordShardTable(accountID int64, deviceNo string) string {
	return apiRequestRecordCfg().shardPhysicalTable(accountID, deviceNo)
	}

// ApiRequestRecordReadPlan 按时间窗 [tStart,tEnd] 相对 T_cut 生成查询计划。
func ApiRequestRecordReadPlan(accountIDs []int64, deviceNo string, tStart, tEnd int64) ReadPlan {
	return apiRequestRecordCfg().resolveReadPlan(accountIDs, deviceNo, tStart, tEnd)
}

// ApiRequestRecordLookupTables 无业务时间定位：切流后先分片再旧表。
func ApiRequestRecordLookupTables(accountID int64, deviceNo string) []string {
	return apiRequestRecordCfg().lookupTables(accountID, deviceNo, time.Now())
	}

// AllApiRequestRecordTables 清理/兜底枚举：未切流仅旧表；切流后旧表+全部分片。
func AllApiRequestRecordTables() []string {
	return apiRequestRecordCfg().allTablesForCleanup()
	}

// AllApiRequestRecordShardTables 仅账户分片物理表（不含旧单表）。
func AllApiRequestRecordShardTables() []string {
	return apiRequestRecordCfg().allShardTables()
	}

// ApiRequestRecordTable 兼容旧调用：按 device_no 已废弃。
// 未切流返回旧单表；已切流仍返回旧单表名并依赖调用方迁移到 WriteTable（避免无 account 误写分片）。
// Deprecated: 请改用 ApiRequestRecordWriteTable(accountID, deviceNo)。
func ApiRequestRecordTable(deviceNo string) string {
	_ = deviceNo
	cfg := apiRequestRecordCfg()
	if !cfg.isCutoverActive(time.Now()) {
		return cfg.TableName
}
	// 切流后旧 API 无法定账户片，返回旧表名会写错；保留返回旧表并让上层尽快改 WriteTable。
	return cfg.TableName
}
