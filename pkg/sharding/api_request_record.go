package sharding

import (
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"sync"
)

const (
	ApiRequestRecordShardModeSingle = "single"
	ApiRequestRecordShardModeHash   = "hash"

	ApiRequestRecordShardCount        = 64
	ApiRequestRecordVirtualShardCount = 1024

	defaultApiRequestRecordTableName   = "api_request_record"
	defaultApiRequestRecordTablePrefix = "api_request_record_"
)

type ApiRequestRecordShardingConfig struct {
	Mode               string
	TableName          string
	TablePrefix        string
	PhysicalShardCount int
	VirtualShardCount  int
}

var (
	apiRequestRecordShardingMu sync.RWMutex
	apiRequestRecordSharding   = DefaultApiRequestRecordShardingConfig()
)

func DefaultApiRequestRecordShardingConfig() ApiRequestRecordShardingConfig {
	return ApiRequestRecordShardingConfig{
		Mode:               ApiRequestRecordShardModeHash,
		TableName:          defaultApiRequestRecordTableName,
		TablePrefix:        defaultApiRequestRecordTablePrefix,
		PhysicalShardCount: ApiRequestRecordShardCount,
		VirtualShardCount:  ApiRequestRecordVirtualShardCount,
	}
}

func ConfigureApiRequestRecordSharding(cfg ApiRequestRecordShardingConfig) error {
	normalized, err := normalizeApiRequestRecordShardingConfig(cfg)
	if err != nil {
		return err
	}
	apiRequestRecordShardingMu.Lock()
	apiRequestRecordSharding = normalized
	apiRequestRecordShardingMu.Unlock()
	return nil
}

func normalizeApiRequestRecordShardingConfig(cfg ApiRequestRecordShardingConfig) (ApiRequestRecordShardingConfig, error) {
	defaultCfg := DefaultApiRequestRecordShardingConfig()
	cfg.Mode = strings.TrimSpace(cfg.Mode)
	cfg.TableName = strings.TrimSpace(cfg.TableName)
	cfg.TablePrefix = strings.TrimSpace(cfg.TablePrefix)
	if cfg.Mode == "" {
		cfg.Mode = defaultCfg.Mode
	}
	if cfg.TableName == "" {
		cfg.TableName = defaultCfg.TableName
	}
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = defaultCfg.TablePrefix
	}
	if cfg.PhysicalShardCount <= 0 {
		cfg.PhysicalShardCount = defaultCfg.PhysicalShardCount
	}
	if cfg.VirtualShardCount <= 0 {
		cfg.VirtualShardCount = defaultCfg.VirtualShardCount
	}
	switch cfg.Mode {
	case ApiRequestRecordShardModeSingle:
		cfg.PhysicalShardCount = 1
	case ApiRequestRecordShardModeHash:
	default:
		return ApiRequestRecordShardingConfig{}, errors.New("invalid api_request_record sharding mode")
	}
	return cfg, nil
}

// ApiRequestRecordTable 根据 deviceNo 路由到具体子表
func ApiRequestRecordTable(deviceNo string) string {
	apiRequestRecordShardingMu.RLock()
	cfg := apiRequestRecordSharding
	apiRequestRecordShardingMu.RUnlock()
	if cfg.Mode == ApiRequestRecordShardModeSingle {
		return cfg.TableName
	}
	if deviceNo == "" {
		return fmt.Sprintf("%s%02d", cfg.TablePrefix, 0)
	}
	return fmt.Sprintf("%s%02d", cfg.TablePrefix, crc32.ChecksumIEEE([]byte(deviceNo))%uint32(cfg.PhysicalShardCount))
}

// AllApiRequestRecordTables 返回全部 api_request_record 表名
func AllApiRequestRecordTables() []string {
	apiRequestRecordShardingMu.RLock()
	cfg := apiRequestRecordSharding
	apiRequestRecordShardingMu.RUnlock()
	if cfg.Mode == ApiRequestRecordShardModeSingle {
		return []string{cfg.TableName}
	}
	tables := make([]string, cfg.PhysicalShardCount)
	for i := range tables {
		tables[i] = fmt.Sprintf("%s%02d", cfg.TablePrefix, i)
	}
	return tables
}
