package scheduler

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/logger"
	pb "github.com/sidchai/compkg/proto/scheduler/v1"
)

// runStreamLoop 是 stream 顶层主循环：建立连接 → 维持 → 断线重连。
//
// 退出条件：rootCtx 被 Stop() cancel。退出前关闭 streamDone 通知 Stop。
func (c *Client) runStreamLoop() {
	defer close(c.streamDone)

	backoff := c.cfg.ReconnectMinBackoff
	for {
		if c.rootCtx.Err() != nil {
			return
		}

		sessionStart := time.Now()
		err := c.runOneSession(c.rootCtx)
		// 正常退出（rootCtx canceled）
		if c.rootCtx.Err() != nil {
			return
		}
		// 稳定运行 >30s 视为握手成功后正常断开，重置退避避免下次飘到 30s
		if time.Since(sessionStart) > 30*time.Second {
			backoff = c.cfg.ReconnectMinBackoff
		}
		if err != nil {
			logger.Warnf("[scheduler-sdk] session ended err=%v, reconnect in %s", err, backoff)
		} else {
			logger.Warnf("[scheduler-sdk] session ended without error, reconnect in %s", backoff)
		}

		// 指数退避（含上限）
		select {
		case <-time.After(backoff):
		case <-c.rootCtx.Done():
			return
		}
		backoff *= 2
		if backoff > c.cfg.ReconnectMaxBackoff {
			backoff = c.cfg.ReconnectMaxBackoff
		}
	}
}

// runOneSession 建立一次 Connect 流：注册 → 起 sender/recv/heartbeat → 阻塞直到任一退出。
func (c *Client) runOneSession(parent context.Context) error {
	sessCtx, cancel := context.WithCancel(parent)
	defer cancel()

	stream, err := c.workerCli.Connect(sessCtx)
	if err != nil {
		return err
	}

	// 1. 发送 RegisterRequest
	creds := newSignedCreds(c.cfg.AppKey, c.cfg.AppSecret)
	if err := stream.Send(&pb.WorkerMessage{
		Payload: &pb.WorkerMessage_Register{
			Register: &pb.RegisterRequest{
				AppName:        c.cfg.AppName,
				AppKey:         c.cfg.AppKey,
				Signature:      creds.Signature,
				Nonce:          creds.Nonce,
				Ts:             creds.Ts,
				WorkerId:       c.cfg.WorkerID,
				InstanceId:     c.cfg.InstanceID,
				Ip:             c.cfg.IP,
				SdkVersion:     c.cfg.SDKVersion,
				HandlerJobs:    c.handlerNames(),
				MaxConcurrency: int32(c.cfg.MaxConcurrency),
			},
		},
	}); err != nil {
		return err
	}

	// 2. 收 RegisterResponse
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	resp := first.GetRegister()
	if resp == nil {
		return errors.New("first server message is not RegisterResponse")
	}
	if !resp.Ok {
		// 鉴权失败：固定错误，外层退避重连仍可能有效（如配置已刷新），但记 error 级
		logger.Errorf("[scheduler-sdk] register denied: %s", resp.Error)
		return errors.New("register denied: " + resp.Error)
	}
	if resp.HeartbeatInterval > 0 {
		c.heartbeatMu.Lock()
		c.negotiatedHbDelay = time.Duration(resp.HeartbeatInterval) * time.Second
		c.heartbeatMu.Unlock()
	}
	logger.Infof("[scheduler-sdk] connected app=%s worker=%s sched_leader=%s hb=%ds",
		c.cfg.AppName, c.cfg.WorkerID, resp.SchedulerLeader, resp.HeartbeatInterval)

	// 3. sendCh + sender goroutine：序列化所有 stream.Send，避免并发写
	sendCh := make(chan *pb.WorkerMessage, 64)
	sendErrCh := make(chan error, 1)
	var senderWG sync.WaitGroup
	senderWG.Add(1)
	go func() {
		defer senderWG.Done()
		for msg := range sendCh {
			if err := stream.Send(msg); err != nil {
				select {
				case sendErrCh <- err:
				default:
				}
				return
			}
		}
	}()

	// 4. heartbeat goroutine：按协商间隔发送 Heartbeat
	hbDone := make(chan struct{})
	go c.runHeartbeat(sessCtx, sendCh, hbDone)

	// 5. recv loop：阻塞接收 SchedulerMessage 并派发
	recvErr := c.runRecvLoop(sessCtx, stream, sendCh)

	// 6. 清理：cancel session → close sendCh → 等 sender 收尾
	cancel()
	close(sendCh)
	<-hbDone
	senderWG.Wait()

	// 区分错误来源：sendErr 优先（更接近底层）
	select {
	case sErr := <-sendErrCh:
		return sErr
	default:
	}

	if errors.Is(recvErr, io.EOF) {
		return nil
	}
	return recvErr
}

// runRecvLoop 持续 stream.Recv 并按 oneof 分发。
//
// 返回值：流自然结束（io.EOF）/ ctx 取消 / 底层 IO 错误。
func (c *Client) runRecvLoop(ctx context.Context, stream pb.WorkerService_ConnectClient, sendCh chan<- *pb.WorkerMessage) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		switch p := msg.Payload.(type) {
		case *pb.SchedulerMessage_Dispatch:
			c.onDispatch(ctx, p.Dispatch, sendCh)
		case *pb.SchedulerMessage_Cancel:
			c.onCancel(ctx, p.Cancel)
		case *pb.SchedulerMessage_Reload:
			c.onReload(ctx, p.Reload)
		case *pb.SchedulerMessage_Register:
			// 不期望握手后再收 RegisterResponse；记 warn 后忽略
			logger.Warnf("[scheduler-sdk] unexpected RegisterResponse after handshake")
		default:
			logger.Warnf("[scheduler-sdk] unknown SchedulerMessage payload type")
		}
	}
}

// runHeartbeat 按 negotiatedHbDelay 定期向 sendCh 推 Heartbeat，直到 ctx 取消。
func (c *Client) runHeartbeat(ctx context.Context, sendCh chan<- *pb.WorkerMessage, done chan struct{}) {
	defer close(done)

	c.heartbeatMu.Lock()
	delay := c.negotiatedHbDelay
	c.heartbeatMu.Unlock()
	if delay <= 0 {
		delay = c.cfg.HeartbeatInterval
	}

	start := time.Now()
	ticker := time.NewTicker(delay)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inflight := int32(c.currentInflight())
			hb := &pb.Heartbeat{
				WorkerId:  c.cfg.WorkerID,
				Ts:        time.Now().Unix(),
				Inflight:  inflight,
				LoadAvg:   0, // load_avg 待接入 metrics 后填充
				UptimeSec: int64(time.Since(start).Seconds()),
			}
			select {
			case sendCh <- &pb.WorkerMessage{Payload: &pb.WorkerMessage_Heartbeat{Heartbeat: hb}}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// currentInflight 当前 inflight handler 数量 = MaxConcurrency - sem 剩余容量。
func (c *Client) currentInflight() int {
	return c.cfg.MaxConcurrency - (cap(c.concurrencySem) - len(c.concurrencySem))
}

// onCancel 处理 server 主动取消：通过 jobCancels 找到对应 handler ctx 并 cancel。
func (c *Client) onCancel(ctx context.Context, m *pb.Cancel) {
	if m == nil || m.RunId == "" {
		return
	}
	c.cancelsMu.Lock()
	cancel, ok := c.cancels[m.RunId]
	c.cancelsMu.Unlock()
	if !ok {
		logger.Warnf("[scheduler-sdk] cancel for unknown run_id=%s reason=%s", m.RunId, m.Reason)
		return
	}
	logger.Infof("[scheduler-sdk] cancel run_id=%s reason=%s", m.RunId, m.Reason)
	cancel()
}

// onReload 服务端 Reload 当前 SDK 是无状态（handler 注册由代码控制），仅记 info。
//
// 预留语义：未来 SDK 可支持动态从 server 拉 job 配置（如 timeout/retry_max）。
func (c *Client) onReload(_ context.Context, m *pb.Reload) {
	if m == nil {
		return
	}
	logger.Infof("[scheduler-sdk] reload received jobs=%d term=%d (currently no-op)", len(m.JobNames), m.Term)
}
