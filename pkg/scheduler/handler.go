package scheduler

import (
	"context"

	pb "github.com/sidchai/compkg/proto/scheduler/v1"
)

// Job 是 scheduler 派发给业务 handler 的任务上下文。
//
// 业务 handler 只读使用本结构，所有字段在 dispatch 时由 SDK 填充。
type Job struct {
	// 任务标识
	RunID   string
	JobName string
	BizKey  string

	// 业务负载，由 SubmitTask / TriggerJob 写入；长度上限由服务端 sched_app.payload_max_bytes 控制
	Payload []byte

	// 触发类型 / 当前重试次数 / 最大重试 / 分片信息
	TriggerType pb.TriggerType
	RetryCount  int32
	RetryMax    int32
	ShardIndex  int32
	ShardTotal  int32

	// 服务端约定的超时（秒），业务可据此控制内部逻辑
	TimeoutSec int32

	// 全链路追踪
	TraceID string
	SpanID  string

	// 派发时刻（unix 秒），用于业务方判断是否陈旧任务
	DispatchedAt int64
}

// HandlerFunc 是业务方实现的任务处理函数。
//
// 约定：
//   - 返回 (output, nil)  → 上报 RUN_STATUS_SUCCESS，output 作为 JobResult.output
//   - 返回 (_, err)       → 上报 RUN_STATUS_FAILED，err.Error() 作为 JobResult.error
//   - ctx 超时             → SDK 检测到 ctx.Err()==DeadlineExceeded 时上报 RUN_STATUS_TIMEOUT
//   - panic               → SDK recover 后上报 RUN_STATUS_FAILED，error 含 panic 值
//
// ctx 由 SDK 派生：含 Job.TimeoutSec 超时 + cancel 信号；handler 必须尊重 ctx.Done()。
type HandlerFunc func(ctx context.Context, job *Job) (output string, err error)
