package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrLocalBufferDisabled 调用 EnqueueTask 但未启用 LocalBufferEnabled。
var ErrLocalBufferDisabled = errors.New("scheduler: local buffer disabled, set Config.LocalBufferEnabled=true")

// ErrLocalBufferFull 兜底队列已满；建议业务方降级（落本地表/丢弃/告警）。
var ErrLocalBufferFull = errors.New("scheduler: local buffer full")

// bufferedTask 队列中的一条待重发任务；保留入队时间用于排障。
type bufferedTask struct {
	opts       SubmitOptions
	enqueuedAt time.Time
	attempts   int
}

// submitBuffer scheduler SDK 的内存级本地兜底队列。
//
// 仅用于 EnqueueTask 路径：业务方在 scheduler 不可达期间仍能"提交"任务，
// 后台 goroutine 在恢复后批量重发；启用磁盘 spill 后，内存满出的任务会进入磁盘二级队列。
type submitBuffer struct {
	cap    int
	mu     sync.Mutex
	queue  []bufferedTask
	notify chan struct{} // 入队事件信号，唤醒 retry worker 立即扫一次
}

func newSubmitBuffer(capacity int) *submitBuffer {
	return &submitBuffer{
		cap:    capacity,
		queue:  make([]bufferedTask, 0, 64),
		notify: make(chan struct{}, 1),
	}
}

// push 入队；满时返回 false。调用方据此报 ErrLocalBufferFull。
func (b *submitBuffer) push(t bufferedTask) bool {
	b.mu.Lock()
	if len(b.queue) >= b.cap {
		b.mu.Unlock()
		return false
	}
	b.queue = append(b.queue, t)
	b.mu.Unlock()
	// 非阻塞唤醒，避免堆积信号
	select {
	case b.notify <- struct{}{}:
	default:
	}
	return true
}

// drain 取出最多 max 条任务用于尝试重发，剩余仍保留在队列中。
func (b *submitBuffer) drain(max int) []bufferedTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) == 0 {
		return nil
	}
	if max <= 0 || max > len(b.queue) {
		max = len(b.queue)
	}
	out := make([]bufferedTask, max)
	copy(out, b.queue[:max])
	b.queue = append(b.queue[:0], b.queue[max:]...)
	return out
}

// putBack 把未成功的任务回插队首，供下一轮再试。
func (b *submitBuffer) putBack(items []bufferedTask) {
	if len(items) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// 容量保护：超过 cap 时丢最旧的，避免无限累积导致内存爆掉
	merged := append(items, b.queue...)
	if len(merged) > b.cap {
		merged = merged[len(merged)-b.cap:]
	}
	b.queue = merged
}

// Len 当前队列长度（暴露给调用方做监控）。
func (b *submitBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}

// EnqueueTask 提交任务到 scheduler；scheduler 不可达时缓存到本地内存队列，
// 由后台 goroutine 在恢复后自动重发。
//
// 语义说明：
//   - 仅当 Config.LocalBufferEnabled=true 时可用，否则返回 ErrLocalBufferDisabled
//   - fire-and-forget：不返回 RunID；业务方应通过 BizKey 实现幂等
//   - 直接成功 → err=nil, queued=false
//   - 直连失败但已缓存 → err=nil, queued=true
//   - 队列已满且未启用磁盘 spill → err=ErrLocalBufferFull
//   - 启用磁盘 spill 后内存满会落盘 → err=nil, queued=true
//   - 入参校验失败 → err 非 nil（不入队）
//
// 与 SubmitTask 的取舍：
//   - 需要立即拿到 RunID / 同步幂等结果 → 用 SubmitTask
//   - 不在意 RunID、想获得"scheduler 短暂抖动也不丢任务"的弹性 → 用 EnqueueTask
func (c *Client) EnqueueTask(ctx context.Context, opts SubmitOptions) (queued bool, err error) {
	if !c.cfg.LocalBufferEnabled || c.localBuffer == nil {
		return false, ErrLocalBufferDisabled
	}
	if opts.JobName == "" {
		return false, errors.New("scheduler: SubmitOptions.JobName required")
	}
	if c.schedCli == nil {
		// 未 Start：直接入队，等 Start 后 worker 启动再发
		if !c.enqueueBufferedTask(bufferedTask{opts: opts, enqueuedAt: time.Now()}) {
			return false, ErrLocalBufferFull
		}
		return true, nil
	}

	// 先尝试直发；只有"连接性"错误才进 buffer，其他错误（例如 InvalidArgument）直接抛给业务方
	if _, _, err = c.SubmitTask(ctx, opts); err == nil {
		return false, nil
	}
	if !isRetriableSubmitErr(err) {
		return false, err
	}
	if !c.enqueueBufferedTask(bufferedTask{opts: opts, enqueuedAt: time.Now()}) {
		return false, ErrLocalBufferFull
	}
	return true, nil
}

// BufferedCount 返回当前本地兜底队列长度，未启用时返回 0；用于业务方暴露监控指标。
func (c *Client) BufferedCount() int {
	if c.localBuffer == nil {
		return 0
	}
	return c.localBuffer.Len()
}

// SpilledCount 返回磁盘二级队列中的待重放任务数量，未启用磁盘 spill 时返回 0。
func (c *Client) SpilledCount() int {
	if c.localSpool == nil {
		return 0
	}
	return c.localSpool.len()
}

// SpilledBytes 返回磁盘二级队列当前占用字节数，未启用磁盘 spill 时返回 0。
func (c *Client) SpilledBytes() int64 {
	if c.localSpool == nil {
		return 0
	}
	return c.localSpool.bytes()
}

// enqueueBufferedTask 优先写入内存队列，内存满时按配置降级到磁盘 spill。
//
// 返回 false 表示内存和磁盘都无法接纳该任务；调用方转换为 ErrLocalBufferFull，
// 以保持历史 API 错误语义兼容。
func (c *Client) enqueueBufferedTask(t bufferedTask) bool {
	if c.localBuffer.push(t) {
		return true
	}
	if c.localSpool == nil {
		return false
	}
	return c.localSpool.push(t) == nil
}

// isRetriableSubmitErr 判定 SubmitTask 错误是否属于"scheduler 端暂时不可达"，
// 这类错误进 buffer；其它错误（业务/参数）直接抛回给调用方，不污染队列。
func isRetriableSubmitErr(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		// 非 gRPC 错误（拨号失败、连接断开包装在 fmt.Errorf 里）按可重试处理
		return true
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled,
		codes.ResourceExhausted, codes.Aborted, codes.Internal:
		return true
	default:
		return false
	}
}

// runBufferRetryLoop 后台 goroutine：定期 + 入队事件触发，把 buffer 里的任务尝试重发。
//
// 退出条件：rootCtx.Done()。每轮最多 cfg.LocalBufferRetryBatch 条；
// 失败的回插队首，等下一轮（避免短暂抖动期反复 hammer）。
func (c *Client) runBufferRetryLoop() {
	defer c.bufferDone()

	ticker := time.NewTicker(c.cfg.LocalBufferRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.rootCtx.Done():
			return
		case <-ticker.C:
		case <-c.localBuffer.notify:
		}
		c.flushBufferOnce()
	}
}

// bufferDone 兜底关闭信号；当前实现仅依赖 rootCtx 即可退出，留作扩展点。
func (c *Client) bufferDone() {}

// flushBufferOnce 单轮重发：取最多 batch 条，逐条发送；失败的整体回插队首。
//
// 注意：一旦遇到第一条 retriable 失败，立即停止本轮（说明 scheduler 仍未恢复），
// 把剩余未尝试 + 失败那条一起回插，避免无效请求风暴。
func (c *Client) flushBufferOnce() {
	if c.schedCli == nil {
		return
	}
	batch := c.localBuffer.drain(c.cfg.LocalBufferRetryBatch)
	if len(batch) == 0 {
		c.flushSpoolOnce()
		return
	}

	for i, t := range batch {
		ctx, cancel := context.WithTimeout(c.rootCtx, c.cfg.SubmitTimeout)
		_, _, err := c.SubmitTask(ctx, t.opts)
		cancel()
		if err == nil {
			continue
		}
		if isRetriableSubmitErr(err) {
			// scheduler 仍不可达：把当前失败 + 后续未尝试任务整体回插
			t.attempts++
			remain := append([]bufferedTask{t}, batch[i+1:]...)
			c.localBuffer.putBack(remain)
			return
		}
		// 非 retriable（业务错误）：丢弃该条，继续处理后续
		// 业务方应通过 SubmitTask 自查；buffer 不会无限重试 InvalidArgument 之类
	}

	// 内存队列清空后再处理磁盘 spill，保持 L1 → L2 的双级顺序；
	// 若磁盘任务仍然不可达，文件保留在磁盘等待下一轮，避免重启丢失。
	c.flushSpoolOnce()
}

// flushSpoolOnce 单轮重放磁盘 spill：按 FIFO 读取 batch 条，成功后 ack 删除文件。
func (c *Client) flushSpoolOnce() {
	if c.localSpool == nil || c.schedCli == nil {
		return
	}
	batch, err := c.localSpool.drain(c.cfg.LocalBufferRetryBatch)
	if err != nil || len(batch) == 0 {
		return
	}
	for _, t := range batch {
		ctx, cancel := context.WithTimeout(c.rootCtx, c.cfg.SubmitTimeout)
		_, _, submitErr := c.SubmitTask(ctx, t.Opts)
		cancel()
		if submitErr == nil {
			_ = c.localSpool.ack(t.Seq)
			continue
		}
		if isRetriableSubmitErr(submitErr) {
			return
		}
		// 非可重试错误说明任务参数/业务状态不合法，删除该 spill 文件避免无限重放。
		_ = c.localSpool.ack(t.Seq)
	}
}
