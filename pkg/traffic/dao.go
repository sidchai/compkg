package traffic

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"gorm.io/gorm/clause"
)

// TrafficHourly 表实体，对应 MySQL 表 device_traffic_hourly。
//
// 注意：total_kb 在 DDL 中是 GENERATED 计算列，不参与 GORM 读写。
type TrafficHourly struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement;column:id"`
	StatTime  time.Time `gorm:"column:stat_time;not null"`
	DeviceNo  string    `gorm:"column:device_no;size:32;not null"`
	Service   string    `gorm:"column:service;size:32;not null"`
	UpKB      int64     `gorm:"column:up_kb;not null;default:0"`
	DownKB    int64     `gorm:"column:down_kb;not null;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// dumpPending 扫描所有"非当前小时"的 Redis Hash，按 (deviceNo, service) 聚合后 UPSERT 到 MySQL。
// 落库成功才删除 Redis Key；失败保留 Key 等下次 cron 重试。
func (t *tracker) dumpPending(ctx context.Context) error {
	keys, err := t.scanHourlyKeys(ctx)
	if err != nil {
		return fmt.Errorf("scan hourly keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	currentKey := t.currentHourKey()
	var processed, skipped, failed int

	for _, key := range keys {
		// 当前小时仍在写入，跳过避免覆盖未完成累加
		if key == currentKey {
			skipped++
			continue
		}

		statTime, err := t.extractHourFromKey(key)
		if err != nil {
			log.Printf("[traffic] skip invalid key %s: %v", key, err)
			skipped++
			continue
		}

		data, err := t.cfg.Redis.HGetAll(ctx, key).Result()
		if err != nil {
			log.Printf("[traffic] hgetall %s failed: %v", key, err)
			failed++
			continue
		}
		if len(data) == 0 {
			// 空 Hash（理论上不会出现，兜底直接删）
			_, _ = t.cfg.Redis.Del(ctx, key).Result()
			continue
		}

		records := aggregateHashToRecords(data, statTime)
		if len(records) == 0 {
			// 全是不合法 field，删除避免下次再处理
			_, _ = t.cfg.Redis.Del(ctx, key).Result()
			continue
		}

		// UPSERT：覆盖式更新（Redis Hash 是该小时全量累计，重跑覆盖等价）
		if err := t.upsertBatch(ctx, records); err != nil {
			log.Printf("[traffic] upsert hour %s failed: %v", statTime.Format(time.RFC3339), err)
			failed++
			continue
		}

		// 落库成功才删 Key
		if _, err := t.cfg.Redis.Del(ctx, key).Result(); err != nil {
			log.Printf("[traffic] del %s after upsert failed (may dup-process next time, upsert is idempotent): %v", key, err)
		}
		processed++
	}

	log.Printf("[traffic] dump done: processed=%d skipped=%d failed=%d total=%d", processed, skipped, failed, len(keys))
	if failed > 0 {
		return fmt.Errorf("dump failed for %d hourly keys", failed)
	}
	return nil
}

// aggregateHashToRecords 把 Redis Hash 数据按 (deviceNo, service) 聚合为 TrafficHourly 切片。
//
// 输入：data[field] = bytes 字符串，field 格式 {deviceNo}|{service}|{up|down}
// 输出：records 字节数已转为 KB（四舍五入）
func aggregateHashToRecords(data map[string]string, statTime time.Time) []TrafficHourly {
	type aggKey struct{ deviceNo, service string }
	type aggVal struct{ up, down int64 }

	agg := make(map[aggKey]*aggVal)
	for field, val := range data {
		deviceNo, service, dir, ok := parseField(field)
		if !ok {
			continue
		}
		bytes, err := strconv.ParseInt(val, 10, 64)
		if err != nil || bytes <= 0 {
			continue
		}
		k := aggKey{deviceNo, service}
		v, exists := agg[k]
		if !exists {
			v = &aggVal{}
			agg[k] = v
		}
		switch dir {
		case "up":
			v.up += bytes
		case "down":
			v.down += bytes
		}
	}

	records := make([]TrafficHourly, 0, len(agg))
	for k, v := range agg {
		records = append(records, TrafficHourly{
			StatTime: statTime,
			DeviceNo: k.deviceNo,
			Service:  k.service,
			UpKB:     bytesToKB(v.up),
			DownKB:   bytesToKB(v.down),
		})
	}
	return records
}

// upsertBatch 批量 UPSERT，唯一键 (stat_time, device_no, service) 冲突时覆盖 up_kb/down_kb。
func (t *tracker) upsertBatch(ctx context.Context, records []TrafficHourly) error {
	if len(records) == 0 {
		return nil
	}
	return t.cfg.DB.WithContext(ctx).
		Table(t.cfg.TableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "stat_time"},
				{Name: "device_no"},
				{Name: "service"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"up_kb", "down_kb"}),
		}).
		Create(&records).Error
}

// bytesToKB 字节转 KB，四舍五入到最近整数。
// <1024 但 >=512 计为 1KB，<512 计为 0KB（与四舍五入一致）。
func bytesToKB(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 512) / 1024
}
