// Package scheduler 是 iot-scheduler 的业务侧客户端 SDK。
//
// 业务方通过本 SDK 完成：
//  1. 注册 Job Handler（业务代码）
//  2. 与 scheduler 服务建立长连接（双向流）：处理服务端派发 + 上报执行结果 + 维持心跳
//  3. 主动提交一次性任务（SubmitTask，trigger_type=api）
//
// 与服务端的协议（compkg/proto/scheduler/v1）：
//   - Connect 双向流：首条 RegisterRequest → 收 RegisterResponse → 循环 Heartbeat/Ack/Result ←→ Dispatch/Cancel/Reload
//   - SubmitTask 单次 unary：业务方主动入队
//
// 签名算法（与 internal/sigverify 保持一致）：
//
//	signature = hex( HMAC-SHA256(appSecret, app_key + nonce + ts) )
//	ts 单位秒，5min 窗口
//
// 典型用法：
//
//	c, err := scheduler.New(scheduler.Config{
//	    Endpoint:       "scheduler:9090",
//	    AppName:        "iot-cloud-platform-open",
//	    AppKey:         os.Getenv("SCHED_APP_KEY"),
//	    AppSecret:      os.Getenv("SCHED_APP_SECRET"),
//	    InstanceID:     hostname,
//	    SDKVersion:     "0.1.0",
//	    MaxConcurrency: 50,
//	})
//	if err != nil { return err }
//	c.RegisterHandler("callback_retry", func(ctx context.Context, j *scheduler.Job) (string, error) {
//	    return doRetry(ctx, j.Payload)
//	})
//	if err := c.Start(ctx); err != nil { return err }
//	defer c.Stop(context.Background())
//
//	// 业务方主动提交
//	runID, err := c.SubmitTask(ctx, scheduler.SubmitOptions{
//	    JobName: "callback_retry",
//	    BizKey:  "record-123",
//	    Payload: []byte(`{"record_id":123}`),
//	})
//
// 线程安全：
//   - 所有公共方法均可并发调用。
//   - RegisterHandler 必须在 Start 之前完成，否则 RegisterRequest.HandlerJobs 不会包含该 job。
package scheduler
