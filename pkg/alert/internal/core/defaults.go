// Package core 存放 P0 告警的领域模型与端口（接口）。
// 无任何 sibling 依赖，避免循环引用。
package core

import "time"

const (
	// DefaultTimeout 单次推送 HTTP 超时，避免发送协程被对端拖死。
	DefaultTimeout = 3 * time.Second
	// DefaultQueueSize 有界队列容量；满则丢，禁止反压业务线程。
	DefaultQueueSize = 1000
	// DefaultRemind 同一 dedupKey 持续 FIRING 时的最短再提醒间隔。
	DefaultRemind = 10 * time.Minute
)
