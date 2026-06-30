package traffic

import (
	"context"
	"strings"
	"time"
)

// scanHourlyKeys SCAN 所有匹配 KeyPrefix* 的小时 Key。
// 用 SCAN 而非 KEYS，避免大库阻塞。
func (t *tracker) scanHourlyKeys(ctx context.Context) ([]string, error) {
	var keys []string
	var cursor uint64
	pattern := t.cfg.KeyPrefix + "*"
	for {
		batch, next, err := t.cfg.Redis.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		if next == 0 {
			break
		}
		cursor = next
	}
	return keys, nil
}

// extractHourFromKey 从 Key 中抽取小时字符串（YYYYMMDDHH）并转 time.Time（Local 时区）。
// 不合法的 Key 返回 error，由调用方决定跳过或上报。
func (t *tracker) extractHourFromKey(key string) (time.Time, error) {
	s := strings.TrimPrefix(key, t.cfg.KeyPrefix)
	return time.ParseInLocation("2006010215", s, time.Local)
}

// currentHourKey 返回当前小时的 Key，dump 时跳过避免覆盖未完成累加。
func (t *tracker) currentHourKey() string {
	return t.cfg.KeyPrefix + time.Now().Format("2006010215")
}

// parseField 解析 Hash field 格式：{deviceNo}|{service}|{up|down}
// 返回 ok=false 表示格式不合法（应跳过该 field）。
func parseField(field string) (deviceNo, service, dir string, ok bool) {
	parts := strings.SplitN(field, "|", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
