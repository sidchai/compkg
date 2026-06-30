package scheduler

import (
	"context"
	"errors"
	"fmt"

	pb "github.com/sidchai/compkg/proto/scheduler/v1"
)

// SubmitOptions 是 SubmitTask 的入参。
//
// 必填：JobName；其余按业务需要传。
type SubmitOptions struct {
	// JobName 必须与服务端 sched_job.job_name 一致，且本应用名下
	JobName string

	// BizKey 业务幂等键；同 BizKey 在 DedupeWindowSec 内只入队一次
	// 服务端命中去重时返回已存在的 RunID，SubmitTaskResponse.Dedup=true
	BizKey string

	// Payload 业务负载，长度上限由 sched_app.payload_max_bytes 限制
	Payload []byte

	// DedupeWindowSec 去重窗口（秒）；0 表示不去重
	DedupeWindowSec int32

	// TraceID / SpanID 透传链路；为空时服务端会自动生成
	TraceID string
	SpanID  string
}

// ErrSubmitNotConnected SubmitTask 在 Start 之前调用。
var ErrSubmitNotConnected = errors.New("scheduler: SubmitTask requires Start() first")

// SubmitTask 主动向 scheduler 提交一次 API 任务，对应 SchedulerService.SubmitTask。
//
// 返回：
//   - runID：scheduler 分配的全局唯一 run_id
//   - dedup：是否命中幂等去重（true 表示返回的是已存在的 run）
//   - err：gRPC 错误或入参错误
//
// 超时：使用 Config.SubmitTimeout（默认 5s）；可通过 ctx 进一步压缩。
func (c *Client) SubmitTask(ctx context.Context, opts SubmitOptions) (runID string, dedup bool, err error) {
	if c.schedCli == nil {
		return "", false, ErrSubmitNotConnected
	}
	if opts.JobName == "" {
		return "", false, errors.New("scheduler: SubmitOptions.JobName required")
	}

	// 应用 Submit 默认超时（若 ctx 没有 deadline）
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.SubmitTimeout)
		defer cancel()
	}

	creds := newSignedCreds(c.cfg.AppKey, c.cfg.AppSecret)
	resp, err := c.schedCli.SubmitTask(ctx, &pb.SubmitTaskRequest{
		AppName:         c.cfg.AppName,
		AppKey:          c.cfg.AppKey,
		Signature:       creds.Signature,
		Nonce:           creds.Nonce,
		Ts:              creds.Ts,
		JobName:         opts.JobName,
		BizKey:          opts.BizKey,
		Payload:         opts.Payload,
		DedupeWindowSec: opts.DedupeWindowSec,
		TraceId:         opts.TraceID,
		SpanId:          opts.SpanID,
	})
	if err != nil {
		return "", false, fmt.Errorf("submit task: %w", err)
	}
	return resp.RunId, resp.Dedup, nil
}

// GetRun 查询 run 状态（透传 SchedulerService.GetRun，方便业务方简单封装）。
func (c *Client) GetRun(ctx context.Context, runID string) (*pb.Run, error) {
	if c.schedCli == nil {
		return nil, ErrSubmitNotConnected
	}
	if runID == "" {
		return nil, errors.New("scheduler: GetRun runID required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.SubmitTimeout)
		defer cancel()
	}
	run, err := c.schedCli.GetRun(ctx, &pb.GetRunRequest{RunId: runID})
	if err != nil {
		return nil, fmt.Errorf("get run %s: %w", runID, err)
	}
	return run, nil
}

// EnsureJob 幂等注册一个 Job：先 GetJob，存在则跳过，不存在则 CreateJob。
//
// 用途：业务方启动时一次性把代码里 RegisterHandler 的 jobName 同步到 scheduler 元数据。
// 注：EnsureJob 不会修改已存在的 Job 配置（避免覆盖运维通过 UI 做的调整）。
// 如需强制更新配置，调用 UpdateJob（未来可补 admin 客户端封装）。
//
// 返回值：
//   - created=true 表示本次新建；created=false 表示已存在
func (c *Client) EnsureJob(ctx context.Context, job *pb.Job) (created bool, err error) {
	if c.schedCli == nil {
		return false, ErrSubmitNotConnected
	}
	if job == nil || job.JobName == "" {
		return false, errors.New("scheduler: EnsureJob job.job_name required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.SubmitTimeout)
		defer cancel()
	}

	// 先查
	_, err = c.schedCli.GetJob(ctx, &pb.GetJobRequest{JobName: job.JobName})
	if err == nil {
		return false, nil // 已存在
	}
	// gRPC NotFound 是预期，进 CreateJob；其他错误直接抛
	if st, ok := grpcStatus(err); ok && st == "NotFound" {
		// fallthrough to create
	} else if !isNotFoundErr(err) {
		return false, fmt.Errorf("get job %s: %w", job.JobName, err)
	}

	// 补默认值：app_name 用 client.AppName，避免业务方每个 Job 都填
	if job.AppName == "" {
		job.AppName = c.cfg.AppName
	}
	if _, err = c.schedCli.CreateJob(ctx, &pb.CreateJobRequest{Job: job}); err != nil {
		// 并发场景：另一实例刚好创建，转为已存在
		if isAlreadyExistsErr(err) {
			return false, nil
		}
		return false, fmt.Errorf("create job %s: %w", job.JobName, err)
	}
	return true, nil
}

// CancelRun 透传 SchedulerService.CancelRun。
func (c *Client) CancelRun(ctx context.Context, runID, reason string) error {
	if c.schedCli == nil {
		return ErrSubmitNotConnected
	}
	if runID == "" {
		return errors.New("scheduler: CancelRun runID required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.SubmitTimeout)
		defer cancel()
	}
	resp, err := c.schedCli.CancelRun(ctx, &pb.CancelRunRequest{RunId: runID, Reason: reason})
	if err != nil {
		return fmt.Errorf("cancel run %s: %w", runID, err)
	}
	if !resp.Ok {
		return fmt.Errorf("cancel run %s: %s", runID, resp.Error)
	}
	return nil
}
