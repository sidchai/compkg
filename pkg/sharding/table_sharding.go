package sharding

import (
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"time"
)

const (
	ShardModeSingle = "single"
	ShardModeHash   = "hash"

	DefaultAccountShardCount       = 64
	DefaultHotspotDeviceShardCount = 8
)

var (
	ErrInvalidAccountID = errors.New("sharding: account_id <= 0 required for shard write")
	ErrInvalidShardMode = errors.New("sharding: invalid mode, expect single|hash")
)

// TableShardingConfig 按账户分片配置（api_request_record / device_log 共用结构）。
//
// cutoverAt=0：未切流，读写仍走 TableName 旧单表。
// cutoverAt>0 且 now>=cutoverAt：新写入走账户分片；查询按时间窗相对 T_cut 路由。
type TableShardingConfig struct {
	Mode                    string
	TableName               string
	TablePrefix             string
	AccountShardCount       int
	CutoverAt               int64
	DeviceShardCount        int
	HotspotAccounts         []int64
	HotspotDeviceShardCount int

	// PhysicalShardCount / VirtualShardCount 为旧 device_no 分片字段，仅作配置兼容：
	// AccountShardCount<=0 时回退读 PhysicalShardCount。
	PhysicalShardCount int
	VirtualShardCount  int
}

// ReadSource 查询目标侧。
type ReadSource int

const (
	ReadLegacy ReadSource = iota // 仅旧单表
	ReadShard                    // 仅账户分片
	ReadBoth                     // 跨 T_cut：旧表 + 分片
)

// ReadPlan 按时间窗相对 T_cut 的查询计划。
type ReadPlan struct {
	Source      ReadSource
	LegacyTable string
	ShardTables []string
	CutoverAt   int64
	// 双侧查询时建议追加的时间裁剪（调用方拼 WHERE）。
	LegacyCreatedAtLt  int64 // >0 时旧侧加 created_at < LegacyCreatedAtLt
	ShardCreatedAtGte  int64 // >0 时分片侧加 created_at >= ShardCreatedAtGte
}

func normalizeTableShardingConfig(cfg TableShardingConfig, defaultTable, defaultPrefix string) (TableShardingConfig, error) {
	cfg.Mode = strings.TrimSpace(strings.ToLower(cfg.Mode))
	cfg.TableName = strings.TrimSpace(cfg.TableName)
	cfg.TablePrefix = strings.TrimSpace(cfg.TablePrefix)
	if cfg.Mode == "" {
		cfg.Mode = ShardModeHash
	}
	if cfg.TableName == "" {
		cfg.TableName = defaultTable
	}
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = defaultPrefix
	}
	if cfg.AccountShardCount <= 0 {
		if cfg.PhysicalShardCount > 0 {
			cfg.AccountShardCount = cfg.PhysicalShardCount
		} else {
			cfg.AccountShardCount = DefaultAccountShardCount
		}
	}
	if cfg.HotspotDeviceShardCount <= 0 {
		cfg.HotspotDeviceShardCount = DefaultHotspotDeviceShardCount
	}
	if cfg.CutoverAt < 0 {
		cfg.CutoverAt = 0
	}
	switch cfg.Mode {
	case ShardModeSingle:
		cfg.AccountShardCount = 1
	case ShardModeHash:
	default:
		return TableShardingConfig{}, ErrInvalidShardMode
	}
	// 规范化热点账户列表
	if len(cfg.HotspotAccounts) > 0 {
		seen := make(map[int64]struct{}, len(cfg.HotspotAccounts))
		hot := make([]int64, 0, len(cfg.HotspotAccounts))
		for _, id := range cfg.HotspotAccounts {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			hot = append(hot, id)
		}
		cfg.HotspotAccounts = hot
	}
	return cfg, nil
}

func (cfg TableShardingConfig) isCutoverActive(at time.Time) bool {
	if cfg.Mode == ShardModeSingle {
		return false
	}
	if cfg.CutoverAt <= 0 {
		return false
	}
	return at.Unix() >= cfg.CutoverAt
}

func (cfg TableShardingConfig) isHotspot(accountID int64) bool {
	for _, id := range cfg.HotspotAccounts {
		if id == accountID {
			return true
		}
	}
	return false
}

func accountShardIndex(accountID int64, n int) int {
	if n <= 0 {
		return 0
	}
	if accountID < 0 {
		accountID = -accountID
	}
	return int(uint64(accountID) % uint64(n))
}

func deviceShardIndex(deviceNo string, n int) int {
	if n <= 0 {
		return 0
	}
	if deviceNo == "" {
		return 0
	}
	return int(crc32.ChecksumIEEE([]byte(deviceNo)) % uint32(n))
}

// shardPhysicalTable 计算账户分片物理表名（不考虑 cutover）。
// 默认 H2：tablePrefix + %02d(account%Na)。
// 热点账户且 HotspotDeviceShardCount>0：tablePrefix + %02d_%02d（账户片 + 设备子片）。
// DeviceShardCount>0 时全局账户+设备（H4，方案默认 0，一般不用）。
func (cfg TableShardingConfig) shardPhysicalTable(accountID int64, deviceNo string) string {
	aa := accountShardIndex(accountID, cfg.AccountShardCount)
	if cfg.isHotspot(accountID) && cfg.HotspotDeviceShardCount > 0 {
		dd := deviceShardIndex(deviceNo, cfg.HotspotDeviceShardCount)
		return fmt.Sprintf("%s%02d_%02d", cfg.TablePrefix, aa, dd)
	}
	if cfg.DeviceShardCount > 0 {
		dd := deviceShardIndex(deviceNo, cfg.DeviceShardCount)
		return fmt.Sprintf("%s%02d_%02d", cfg.TablePrefix, aa, dd)
	}
	return fmt.Sprintf("%s%02d", cfg.TablePrefix, aa)
}

func (cfg TableShardingConfig) writeTable(accountID int64, deviceNo string, at time.Time) (string, error) {
	if !cfg.isCutoverActive(at) {
		return cfg.TableName, nil
	}
	if accountID <= 0 {
		return "", ErrInvalidAccountID
	}
	return cfg.shardPhysicalTable(accountID, deviceNo), nil
}

// resolveReadPlan 按 [tStart, tEnd] 相对 T_cut 决定查旧表 / 分片 / 双侧。
// accountIDs 为空且需要分片时返回全部账户片（仅应急；业务应尽量带 account）。
func (cfg TableShardingConfig) resolveReadPlan(accountIDs []int64, deviceNo string, tStart, tEnd int64) ReadPlan {
	plan := ReadPlan{
		Source:      ReadLegacy,
		LegacyTable: cfg.TableName,
		CutoverAt:   cfg.CutoverAt,
	}
	if cfg.Mode == ShardModeSingle || cfg.CutoverAt <= 0 {
		return plan
	}
	cut := cfg.CutoverAt
	switch {
	case tEnd > 0 && tEnd < cut:
		plan.Source = ReadLegacy
		return plan
	case tStart >= cut:
		plan.Source = ReadShard
		plan.ShardTables = cfg.shardTablesForAccounts(accountIDs, deviceNo)
		return plan
	case tStart > 0 && tStart < cut && tEnd >= cut:
		plan.Source = ReadBoth
		plan.LegacyCreatedAtLt = cut
		plan.ShardCreatedAtGte = cut
		plan.ShardTables = cfg.shardTablesForAccounts(accountIDs, deviceNo)
		return plan
	case tStart <= 0 && tEnd <= 0:
		// 无时间窗：切流后默认双侧（旧表全量 + 分片），由调用方强制默认窗后再调更佳
		plan.Source = ReadBoth
		plan.LegacyCreatedAtLt = cut
		plan.ShardCreatedAtGte = cut
		plan.ShardTables = cfg.shardTablesForAccounts(accountIDs, deviceNo)
		return plan
	case tStart <= 0 && tEnd >= cut:
		plan.Source = ReadBoth
		plan.LegacyCreatedAtLt = cut
		plan.ShardCreatedAtGte = cut
		plan.ShardTables = cfg.shardTablesForAccounts(accountIDs, deviceNo)
		return plan
	case tStart < cut && tEnd <= 0:
		plan.Source = ReadBoth
		plan.LegacyCreatedAtLt = cut
		plan.ShardCreatedAtGte = cut
		plan.ShardTables = cfg.shardTablesForAccounts(accountIDs, deviceNo)
		return plan
	default:
		// tEnd < cut 已覆盖；其余偏切流前
		plan.Source = ReadLegacy
		return plan
	}
}

func (cfg TableShardingConfig) shardTablesForAccounts(accountIDs []int64, deviceNo string) []string {
	if len(accountIDs) == 0 {
		return cfg.allShardTables()
	}
	set := make(map[string]struct{}, len(accountIDs))
	out := make([]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		name := cfg.shardPhysicalTable(id, deviceNo)
		if _, ok := set[name]; ok {
			continue
		}
		set[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return cfg.allShardTables()
	}
	return out
}

func (cfg TableShardingConfig) allShardTables() []string {
	if cfg.Mode == ShardModeSingle {
		return nil
	}
	// 热点子片：枚举基础账户片 + 热点账户的设备子片
	n := cfg.AccountShardCount
	tables := make([]string, 0, n+len(cfg.HotspotAccounts)*cfg.HotspotDeviceShardCount)
	baseSet := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s%02d", cfg.TablePrefix, i)
		tables = append(tables, name)
		baseSet[name] = struct{}{}
	}
	if cfg.DeviceShardCount > 0 {
		// 全局 H4：每账户片下再挂设备子片，清理需枚举
		out := make([]string, 0, n*cfg.DeviceShardCount)
		for aa := 0; aa < n; aa++ {
			for dd := 0; dd < cfg.DeviceShardCount; dd++ {
				out = append(out, fmt.Sprintf("%s%02d_%02d", cfg.TablePrefix, aa, dd))
			}
		}
		return out
	}
	for _, accountID := range cfg.HotspotAccounts {
		aa := accountShardIndex(accountID, cfg.AccountShardCount)
		for dd := 0; dd < cfg.HotspotDeviceShardCount; dd++ {
			name := fmt.Sprintf("%s%02d_%02d", cfg.TablePrefix, aa, dd)
			if _, ok := baseSet[name]; ok {
				continue
			}
			tables = append(tables, name)
		}
	}
	return tables
}

func (cfg TableShardingConfig) allTablesForCleanup() []string {
	if cfg.Mode == ShardModeSingle || cfg.CutoverAt <= 0 {
		return []string{cfg.TableName}
	}
	tables := []string{cfg.TableName}
	tables = append(tables, cfg.allShardTables()...)
	return tables
}

// lookupTables 无业务时间时的定位顺序：切流后先分片再旧表；未切流仅旧表。
func (cfg TableShardingConfig) lookupTables(accountID int64, deviceNo string, at time.Time) []string {
	if !cfg.isCutoverActive(at) {
		return []string{cfg.TableName}
	}
	if accountID <= 0 {
		// 无账户时无法定片，只能扫旧表兜底（调用方应尽量补 account）
		return []string{cfg.TableName}
	}
	return []string{cfg.shardPhysicalTable(accountID, deviceNo), cfg.TableName}
}
