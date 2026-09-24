package core

import (
	"context"
	"time"
)

// PipelineTracker 录音/日志阶段交接：起点写标记，终点删除，超时后按主键点查终态。
// 实现用 Redis ZSET（score=截止时间），禁止扫业务大表。
type PipelineTracker interface {
	MarkStart(ctx context.Context, stage, bizKey string) error
	MarkEnd(ctx context.Context, stage, bizKey string) error
	ScanTimeouts(ctx context.Context, now time.Time, confirm ConfirmFunc) []PendingItem
	ScanStage(ctx context.Context, stage string, now time.Time, confirm ConfirmFunc) []PendingItem
}

// ConfirmFunc 超时后按主键/jobId/record_name 点查，禁止扫大表。
type ConfirmFunc func(ctx context.Context, item PendingItem) (finished bool, err error)

// PendingItem 待确认阶段。
type PendingItem struct {
	Stage     string
	BizKey    string
	StartedAt time.Time
	Deadline  time.Time
}

// StageTimeout 阶段时限。
type StageTimeout struct {
	Wait         time.Duration
	ConfirmDelay time.Duration
}

// DefaultStageTimeouts 文档 3.2 的四阶段时限。
func DefaultStageTimeouts() map[string]StageTimeout {
	return map[string]StageTimeout{
		StageCreateTranscodeJob: {Wait: 10 * time.Second, ConfirmDelay: 5 * time.Second},
		StageTranscode:          {Wait: 30 * time.Minute},
		StagePlatformReady:      {Wait: 10 * time.Second, ConfirmDelay: 5 * time.Second},
		StageCallbackDispatch:   {Wait: 40 * time.Second},
		StageLogProcess:         {Wait: 10 * time.Second, ConfirmDelay: 5 * time.Second},
		StageInstructionAck:     {Wait: 32 * time.Second},
	}
}
