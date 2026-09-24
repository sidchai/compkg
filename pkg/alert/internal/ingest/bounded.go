// Package ingest 给心跳等热路径提供 O(1) 非阻塞摄入。
package ingest

import (
	"sync"
	"sync/atomic"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// Bounded 有界 channel + worker。
type Bounded struct {
	ch      chan core.Sample
	dropped atomic.Int64
	handle  func(core.Sample)
	once    sync.Once
	closed  atomic.Bool
}

// New 启动固定 worker。handle 在 worker 内执行，禁止再做同步 HTTP。
func New(queueSize, workers int, handle func(core.Sample)) *Bounded {
	if queueSize <= 0 {
		queueSize = 4096
	}
	if workers <= 0 {
		workers = 4
	}
	b := &Bounded{
		ch:     make(chan core.Sample, queueSize),
		handle: handle,
	}
	for i := 0; i < workers; i++ {
		go b.loop()
	}
	return b
}

func (b *Bounded) loop() {
	for s := range b.ch {
		if b.handle != nil {
			b.handle(s)
		}
	}
}

// Ingest 使用 select default，O(1) 且不阻塞心跳路径。
func (b *Bounded) Ingest(sample core.Sample) {
	if b == nil || b.closed.Load() {
		return
	}
	select {
	case b.ch <- sample:
	default:
		b.dropped.Add(1)
	}
}

func (b *Bounded) Dropped() int64 { return b.dropped.Load() }

func (b *Bounded) Close() {
	if b == nil || !b.closed.CompareAndSwap(false, true) {
		return
	}
	b.once.Do(func() { close(b.ch) })
}
