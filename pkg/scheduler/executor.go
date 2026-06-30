package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/sidchai/compkg/pkg/logger"
	pb "github.com/sidchai/compkg/proto/scheduler/v1"
)

// 私有：jobCancels 是 Client 的一部分，但放在 executor.go 集中讲清楚生命周期。
//
// 设计：每条 Dispatch 启动一个独立 goroutine 执行 handler；
//   - acquire semaphore 失败 → 立即 Ack(accepted=false, reason="inflight full")
//   - acquire 成功 → Ack(accepted=true) → 跑 handler（带超时） → 上报 JobResult → release semaphore
//   - Cancel 通过 jobCancels[runID] 找到对应 ctx.cancel

// jobCancels 在 Client 上下文存活；这里给个 init 辅助，由 Client 首次使用时懒初始化。
// 直接在 Client 结构体定义会让 client.go 变长，按 SRP 拆到这里。
func (c *Client) initCancelsOnce() {
	c.cancelsOnce.Do(func() {
		c.cancels = make(map[string]context.CancelFunc)
	})
}

// onDispatch 处理一条服务端派发的任务。
//
// 关键约束：
//   - 必须立刻 Ack（accepted=true / false），否则 server 端 sched_run 卡在 dispatched 状态
//   - acquire 失败 → Ack(accepted=false, reason)，不算业务失败，server 端不会 retry
//   - acquire 成功后失败：通过 JobResult 上报 RUN_STATUS_FAILED，server 端按 retry_max 评估
func (c *Client) onDispatch(parent context.Context, d *pb.Dispatch, sendCh chan<- *pb.WorkerMessage) {
	if d == nil || d.RunId == "" {
		logger.Warnf("[scheduler-sdk] dispatch with empty run_id, drop")
		return
	}

	handler := c.lookupHandler(d.JobName)
	if handler == nil {
		// 未注册 → 直接 Ack(false)，避免服务端 timeout 等待
		sendOrDrop(sendCh, parent, &pb.WorkerMessage{Payload: &pb.WorkerMessage_Ack{
			Ack: &pb.JobAck{RunId: d.RunId, Accepted: false, Reason: "handler not registered: " + d.JobName},
		}})
		logger.Warnf("[scheduler-sdk] dispatch unknown job=%s run_id=%s", d.JobName, d.RunId)
		return
	}

	// 尝试 acquire（非阻塞）；满了直接 Ack(false)
	select {
	case c.concurrencySem <- struct{}{}:
	default:
		sendOrDrop(sendCh, parent, &pb.WorkerMessage{Payload: &pb.WorkerMessage_Ack{
			Ack: &pb.JobAck{RunId: d.RunId, Accepted: false, Reason: "inflight full"},
		}})
		logger.Warnf("[scheduler-sdk] reject run_id=%s reason=inflight full", d.RunId)
		return
	}

	// 立刻 Ack(accepted=true)
	sendOrDrop(sendCh, parent, &pb.WorkerMessage{Payload: &pb.WorkerMessage_Ack{
		Ack: &pb.JobAck{RunId: d.RunId, Accepted: true},
	}})

	// 派生 handler ctx：超时优先用 dispatch.TimeoutSec，否则不超时（由业务自控）
	var handlerCtx context.Context
	var handlerCancel context.CancelFunc
	if d.TimeoutSec > 0 {
		handlerCtx, handlerCancel = context.WithTimeout(parent, time.Duration(d.TimeoutSec)*time.Second)
	} else {
		handlerCtx, handlerCancel = context.WithCancel(parent)
	}

	// 注册取消句柄，供 onCancel 找到
	c.initCancelsOnce()
	c.cancelsMu.Lock()
	c.cancels[d.RunId] = handlerCancel
	c.cancelsMu.Unlock()

	c.inflightWG.Add(1)
	go func() {
		defer c.inflightWG.Done()
		defer func() {
			handlerCancel()
			c.cancelsMu.Lock()
			delete(c.cancels, d.RunId)
			c.cancelsMu.Unlock()
			<-c.concurrencySem
		}()

		startedAt := time.Now()
		job := &Job{
			RunID:        d.RunId,
			JobName:      d.JobName,
			BizKey:       d.BizKey,
			Payload:      d.Payload,
			TriggerType:  d.TriggerType,
			RetryCount:   d.RetryCount,
			RetryMax:     d.RetryMax,
			ShardIndex:   d.ShardIndex,
			ShardTotal:   d.ShardTotal,
			TimeoutSec:   d.TimeoutSec,
			TraceID:      d.TraceId,
			SpanID:       d.SpanId,
			DispatchedAt: d.DispatchedAt,
		}

		output, err := runHandlerSafe(handlerCtx, handler, job)
		endedAt := time.Now()

		status := pb.RunStatus_RUN_STATUS_SUCCESS
		errStr := ""
		if err != nil {
			// 区分 timeout / canceled / 业务失败
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				status = pb.RunStatus_RUN_STATUS_TIMEOUT
			case errors.Is(err, context.Canceled):
				// server 主动 Cancel 或 Stop()
				status = pb.RunStatus_RUN_STATUS_CANCELED
			default:
				status = pb.RunStatus_RUN_STATUS_FAILED
			}
			errStr = err.Error()
		}

		duration := endedAt.Sub(startedAt)
		sendOrDrop(sendCh, parent, &pb.WorkerMessage{Payload: &pb.WorkerMessage_Result{
			Result: &pb.JobResult{
				RunId:      d.RunId,
				Status:     status,
				Output:     output,
				Error:      errStr,
				DurationMs: int32(duration.Milliseconds()),
				StartedAt:  startedAt.UnixMilli(),
				EndedAt:    endedAt.UnixMilli(),
			},
		}})
	}()
}

// runHandlerSafe 执行 handler 并 recover panic，把 panic 转成 error。
func runHandlerSafe(ctx context.Context, h HandlerFunc, job *Job) (output string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v\n%s", r, debug.Stack())
			output = ""
		}
	}()
	return h(ctx, job)
}

// sendOrDrop 把 msg 投到 sendCh；ctx 已取消则丢弃（log warn）。
//
// 用于 onDispatch 中的 Ack/Result：如果 session 在派发完成前结束，结果无法回传，
// 服务端会通过 timeout 兜底（dispatched 状态下经过 sched_job.timeout_seconds 后落到 timeout）。
func sendOrDrop(ch chan<- *pb.WorkerMessage, ctx context.Context, msg *pb.WorkerMessage) {
	select {
	case ch <- msg:
	case <-ctx.Done():
		logger.Warnf("[scheduler-sdk] drop outbound message, session ended")
	}
}
