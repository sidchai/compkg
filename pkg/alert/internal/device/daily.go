package device

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// naturalDayLoc 自然日按东八区零点切换。用固定时区，避免容器缺少 tzdata 时 LoadLocation 失败。
var naturalDayLoc = time.FixedZone("UTC+8", 8*3600)

// dayCount 某条规则在一个自然日内已经触发的次数。
type dayCount struct {
	day string
	n   int
}

func naturalDay(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.In(naturalDayLoc).Format("2006-01-02")
}

// ttlUntilNextNaturalDay 键活到下一个东八区零点。过期后次日可以重新占用名额。
func ttlUntilNextNaturalDay(t time.Time) time.Duration {
	if t.IsZero() {
		t = time.Now()
	}
	local := t.In(naturalDayLoc)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, naturalDayLoc)
	d := next.Sub(local)
	if d < time.Second {
		d = time.Second
	}
	return d
}

// dailyFireKey 不放明文设备号。sub 只接受 sdcard_low / upload_backlog。
func dailyFireKey(day, sub, deviceNo string) string {
	return "p0:devday:" + day + ":" + sub + ":" + core.HashID(deviceNo)
}

func dailyLimited(sub string) bool {
	return sub == core.SubSDCardLow || sub == core.SubUploadBacklog
}

// keepWithinDaily 只留下这次允许发出的命中。温度、电量、重连不占自然日名额。
func (e *Engine) keepWithinDaily(st *deviceState, deviceNo string, at time.Time, hits []deviceHit) []deviceHit {
	if len(hits) == 0 {
		return hits
	}
	kept := make([]deviceHit, 0, len(hits))
	for _, h := range hits {
		if dailyLimited(h.subType) && !e.claimDaily(st, deviceNo, h.subType, at) {
			continue
		}
		kept = append(kept, h)
	}
	return kept
}

// claimDaily 为 SD 不足或待上传积压占用一次自然日名额。
// 已达上限返回 false，调用方不得把这条规则再写进告警。
// 存储失败时退回本进程计数，避免 Redis 抖动吞掉本该发出的告警。
func (e *Engine) claimDaily(st *deviceState, deviceNo, sub string, at time.Time) bool {
	if e == nil || st == nil {
		return true
	}
	limit := e.th.DeviceDailyFireLimit
	if limit <= 0 {
		return true
	}
	if st.daily == nil {
		st.daily = make(map[string]dayCount, 2)
	}
	day := naturalDay(at)
	rec := st.daily[sub]
	if rec.day != day {
		rec = dayCount{day: day}
	}
	if rec.n >= limit {
		st.daily[sub] = rec
		return false
	}
	next := rec.n + 1
	if e.store != nil {
		n, blocked, degraded := e.claimDailyStore(deviceNo, sub, day, at, next, limit)
		if blocked {
			rec.n = limit
			st.daily[sub] = rec
			return false
		}
		if !degraded {
			rec.n = n
			st.daily[sub] = rec
			return true
		}
	}
	rec.n = next
	st.daily[sub] = rec
	return true
}

// claimDailyStore 用 SetNX 抢当日第一次，保证多实例默认「一天一次」不会双发。
// 次数大于 1 时后续用读改写，极端并发可能多告一次，不丢告警。
func (e *Engine) claimDailyStore(deviceNo, sub, day string, at time.Time, next, limit int) (n int, blocked, degraded bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	key := dailyFireKey(day, sub, deviceNo)
	ttl := ttlUntilNextNaturalDay(at)
	if next == 1 {
		ok, err := e.store.SetNX(ctx, key, "1", ttl)
		if err != nil {
			slog.Warn("p0alert 设备自然日限次写入失败，降级本机计数", "err", err, "sub", sub)
			return 0, false, true
		}
		if ok {
			return 1, false, false
		}
		v, err := e.store.Get(ctx, key)
		if err != nil {
			slog.Warn("p0alert 设备自然日限次读取失败，降级本机计数", "err", err, "sub", sub)
			return 0, false, true
		}
		cur, convErr := strconv.Atoi(v)
		if convErr != nil || cur <= 0 {
			cur = 1
		}
		if cur >= limit {
			return cur, true, false
		}
		cur++
		if err = e.store.Set(ctx, key, strconv.Itoa(cur), ttl); err != nil {
			slog.Warn("p0alert 设备自然日限次更新失败，降级本机计数", "err", err, "sub", sub)
			return 0, false, true
		}
		return cur, false, false
	}
	if err := e.store.Set(ctx, key, strconv.Itoa(next), ttl); err != nil {
		slog.Warn("p0alert 设备自然日限次更新失败，降级本机计数", "err", err, "sub", sub)
		return 0, false, true
	}
	return next, false, false
}
