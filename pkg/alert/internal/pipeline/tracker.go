// Package pipeline 用短 TTL 标记做阶段交接，超时后必须点查终态再告警。
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// Tracker 阶段交接实现。
type Tracker struct {
	store    core.StateStore
	timeouts map[string]core.StageTimeout
	prefix   string
	logger   *slog.Logger
	Now      func() time.Time // 可注入，便于单测卡死超时边界
}

// New 创建阶段跟踪器。store 为 nil 时 Mark 直接成功，避免监控拖垮业务。
func New(st core.StateStore, logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracker{
		store:    st,
		timeouts: core.DefaultStageTimeouts(),
		prefix:   "p0alert:pipe",
		logger:   logger,
		Now:      time.Now,
	}
}

// markKey 形如 p0alert:pipe:k:{stage}:{bizHash}，只存起点时间，TTL 略长于确认窗口。
func (p *Tracker) markKey(stage, bizKey string) string {
	return p.prefix + ":k:" + stage + ":" + core.HashID(bizKey)
}

// zKey 形如 p0alert:pipe:z:{stage}，score 为 Unix 截止时间，member 带明文 bizKey 供回查。
func (p *Tracker) zKey(stage string) string {
	return p.prefix + ":z:" + stage
}

// MarkStart 写入待确认标记。失败只打自检日志，调用方应忽略 error。
func (p *Tracker) MarkStart(ctx context.Context, stage, bizKey string) error {
	if p.store == nil || stage == "" || bizKey == "" {
		return nil
	}
	spec, ok := p.timeouts[stage]
	if !ok {
		spec = core.StageTimeout{Wait: 10 * time.Second}
	}
	now := p.Now()
	deadline := now.Add(spec.Wait + spec.ConfirmDelay)
	val := fmt.Sprintf("%d", now.Unix())
	if err := p.store.Set(ctx, p.markKey(stage, bizKey), val, spec.Wait+spec.ConfirmDelay+2*time.Minute); err != nil {
		p.logger.Error("p0alert mark start failed", "stage", stage, "err", err)
		return err
	}
	if err := p.store.ZAdd(ctx, p.zKey(stage), float64(deadline.Unix()), core.HashID(bizKey)+"|"+bizKey); err != nil {
		p.logger.Error("p0alert mark zadd failed", "stage", stage, "err", err)
		return err
	}
	return nil
}

// MarkEnd 删除标记。终点已完成后即使超时扫描扫到也会被 Confirm 判为 finished。
func (p *Tracker) MarkEnd(ctx context.Context, stage, bizKey string) error {
	if p.store == nil || stage == "" || bizKey == "" {
		return nil
	}
	_ = p.store.Del(ctx, p.markKey(stage, bizKey))
	_ = p.store.ZRem(ctx, p.zKey(stage), core.HashID(bizKey)+"|"+bizKey)
	return nil
}

func (p *Tracker) ScanTimeouts(ctx context.Context, now time.Time, confirm core.ConfirmFunc) []core.PendingItem {
	out := make([]core.PendingItem, 0)
	for stage := range p.timeouts {
		out = append(out, p.ScanStage(ctx, stage, now, confirm)...)
	}
	return out
}

// ScanStage 只扫已到期成员。confirm 返回 finished 则删标记且不告警，避免标记残留误报。
func (p *Tracker) ScanStage(ctx context.Context, stage string, now time.Time, confirm core.ConfirmFunc) []core.PendingItem {
	if p.store == nil || stage == "" {
		return nil
	}
	members, err := p.store.ZRangeByScore(ctx, p.zKey(stage), 0, float64(now.Unix()), 200)
	if err != nil {
		p.logger.Error("p0alert scan timeouts failed", "stage", stage, "err", err)
		return nil
	}
	out := make([]core.PendingItem, 0)
	for _, m := range members {
		bizKey := m.Member
		if i := indexByte(m.Member, '|'); i >= 0 {
			bizKey = m.Member[i+1:]
		}
		item := core.PendingItem{
			Stage:    stage,
			BizKey:   bizKey,
			Deadline: time.Unix(int64(m.Score), 0),
		}
		if confirm != nil {
			finished, cerr := confirm(ctx, item)
			if cerr != nil {
				p.logger.Error("p0alert confirm failed", "stage", stage, "err", cerr)
				continue
			}
			if finished {
				_ = p.MarkEnd(ctx, stage, bizKey)
				continue
			}
		}
		out = append(out, item)
		_ = p.MarkEnd(ctx, stage, bizKey)
	}
	return out
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
