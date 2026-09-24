package core

import "context"

// Sender 告警出口端口。业务只 Enqueue，不得直接 HTTP。
type Sender interface {
	Send(ctx context.Context, event Event) error
	Name() string
}

// NopSender 关闭告警或单测使用，不发网络请求。
type NopSender struct{}

func (NopSender) Send(context.Context, Event) error { return nil }
func (NopSender) Name() string                      { return "nop" }
