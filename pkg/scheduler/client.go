package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/sidchai/compkg/proto/scheduler/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// ErrAlreadyStarted 重复调用 Start 时返回。
var ErrAlreadyStarted = errors.New("scheduler: client already started")

// ErrNotStarted Stop / SubmitTask 在 Start 之前调用时返回。
var ErrNotStarted = errors.New("scheduler: client not started")

// Client 业务侧 SDK 客户端。
//
// 生命周期：
//
//	New(cfg) → RegisterHandler(...) → Start(ctx) → ... → Stop(stopCtx)
//
// 一个进程通常只创建一个 Client；多 AppName 场景请创建多个独立实例。
type Client struct {
	cfg Config

	// gRPC 资源
	conn      *grpc.ClientConn
	workerCli pb.WorkerServiceClient
	schedCli  pb.SchedulerServiceClient

	// handlers：jobName → HandlerFunc，Start 之前注册；Start 后只读
	handlersMu sync.RWMutex
	handlers   map[string]HandlerFunc

	// 服务端协商后的实际心跳间隔（RegisterResponse.HeartbeatInterval），运行时由 stream 写入
	heartbeatMu       sync.Mutex
	negotiatedHbDelay time.Duration

	// 运行时状态
	startedMu sync.Mutex
	started   bool

	// 总生命周期 ctx：Start 时创建，Stop 时 cancel；stream/heartbeat/executor 都 derive from 它
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// inflight：保护优雅关闭时等待 handler 跑完
	inflightWG sync.WaitGroup

	// 并发限制 semaphore（buffered chan 实现）
	concurrencySem chan struct{}

	// stream 子 goroutine 退出信号，Stop 时等它收尾
	streamDone chan struct{}

	// cancels：run_id → handler ctx 的 cancel 函数；用于响应服务端 Cancel
	cancelsOnce sync.Once
	cancelsMu   sync.Mutex
	cancels     map[string]context.CancelFunc

	// localBuffer 本地兜底队列；仅当 cfg.LocalBufferEnabled=true 时非 nil
	localBuffer *submitBuffer

	// localSpool 磁盘二级兜底队列；仅当 LocalBufferEnabled 和 LocalBufferDiskSpillEnabled 同时为 true 时非 nil
	localSpool *submitSpool
}

// New 构造一个未启动的 Client；handler 注册完毕后调用 Start。
func New(cfg Config) (*Client, error) {
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cli := &Client{
		cfg:               cfg,
		handlers:          make(map[string]HandlerFunc),
		concurrencySem:    make(chan struct{}, cfg.MaxConcurrency),
		negotiatedHbDelay: cfg.HeartbeatInterval,
	}
	if cfg.LocalBufferEnabled {
		// 提前初始化，业务方在 Start 之前也能 EnqueueTask 进队
		cli.localBuffer = newSubmitBuffer(cfg.LocalBufferCapacity)
		if cfg.LocalBufferDiskSpillEnabled {
			spool, err := newSubmitSpool(cfg.LocalBufferDiskSpillDir, cfg.LocalBufferDiskSpillMaxBytes)
			if err != nil {
				return nil, err
			}
			cli.localSpool = spool
		}
	}
	return cli, nil
}

// RegisterHandler 注册一个 Job 的处理函数。必须在 Start 之前调用。
//
// 重复注册同一 jobName 会覆盖旧值。jobName 为空或 h 为 nil 直接 panic（属编码错误）。
func (c *Client) RegisterHandler(jobName string, h HandlerFunc) {
	if jobName == "" {
		panic("scheduler: RegisterHandler with empty jobName")
	}
	if h == nil {
		panic("scheduler: RegisterHandler with nil handler")
	}
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[jobName] = h
}

// handlerNames 返回当前注册的所有 jobName，用于 RegisterRequest.HandlerJobs。
func (c *Client) handlerNames() []string {
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()
	names := make([]string, 0, len(c.handlers))
	for name := range c.handlers {
		names = append(names, name)
	}
	return names
}

// lookupHandler 派发时根据 jobName 查 handler；未注册返回 nil。
func (c *Client) lookupHandler(jobName string) HandlerFunc {
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()
	return c.handlers[jobName]
}

// Start 建立到 scheduler 的 gRPC 连接并启动后台 stream goroutine。
//
// 非阻塞：返回后业务可继续。stream 内部自带重连，连不上会持续重试直到 ctx 取消。
// 重复调用返回 ErrAlreadyStarted。
func (c *Client) Start(ctx context.Context) error {
	c.startedMu.Lock()
	if c.started {
		c.startedMu.Unlock()
		return ErrAlreadyStarted
	}
	c.started = true
	c.startedMu.Unlock()

	// 拨号；用 ctx 控制 DialTimeout
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.DialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, c.cfg.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(c.cfg.MaxRecvMsgSizeMB*1024*1024),
			grpc.MaxCallSendMsgSize(c.cfg.MaxSendMsgSizeMB*1024*1024),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		c.startedMu.Lock()
		c.started = false
		c.startedMu.Unlock()
		return fmt.Errorf("scheduler: dial %s: %w", c.cfg.Endpoint, err)
	}
	c.conn = conn
	c.workerCli = pb.NewWorkerServiceClient(conn)
	c.schedCli = pb.NewSchedulerServiceClient(conn)

	// 总 ctx：Stop 时 cancel
	c.rootCtx, c.rootCancel = context.WithCancel(context.Background())
	c.streamDone = make(chan struct{})
	go c.runStreamLoop()

	// 本地兜底 buffer 启用时，启动后台重发 worker；rootCtx cancel 时退出
	if c.localBuffer != nil {
		go c.runBufferRetryLoop()
	}

	return nil
}

// Stop 优雅关闭：取消 stream + 等待 inflight handler 跑完 + 关闭 grpc.ClientConn。
//
// ctx 超时后强制返回（已派发但未完成的任务会被丢弃，不上报 result）。
// 重复调用安全：第二次起返回 nil。
func (c *Client) Stop(ctx context.Context) error {
	c.startedMu.Lock()
	if !c.started {
		c.startedMu.Unlock()
		return nil
	}
	c.started = false
	c.startedMu.Unlock()

	// 1. 让 stream 主循环退出
	if c.rootCancel != nil {
		c.rootCancel()
	}

	// 2. 等 stream goroutine 收尾（含 server-side Unregister 尝试）
	if c.streamDone != nil {
		select {
		case <-c.streamDone:
		case <-ctx.Done():
		}
	}

	// 3. 等所有 inflight handler 完成
	done := make(chan struct{})
	go func() {
		c.inflightWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}

	// 4. 关闭连接
	if c.conn != nil {
		_ = c.conn.Close()
	}
	return nil
}
