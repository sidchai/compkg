package core

import "context"

// PushLogStore 推送流水。只在 Reporter 发送协程调用，禁止进业务热路径。
// 写失败不得影响 webhook，也不得回滚业务。
type PushLogStore interface {
	Save(ctx context.Context, ev Event, sendOK bool, sendErr string) error
	EnsureTable(ctx context.Context) error
}

// ExecFunc 执行一条参数化 SQL。Gorm / Xorm 各自适配，避免公共库绑死 ORM。
type ExecFunc func(ctx context.Context, query string, args ...any) error
