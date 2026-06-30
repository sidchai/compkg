// Package buildinfo 保存编译期注入的版本指纹信息，供 compkg 下所有服务统一复用。
//
// 设计目的：发版后需要可靠区分"正在运行的进程"是否就是"本次发布的二进制"。
// 进程无法在编译期算出自己最终二进制的 sha256（先有鸡先有蛋），因此用
// GitCommit + BuildTime 作为源码版本指纹，由 -ldflags 在编译时注入。
//
// 注入方式（所有服务编译脚本统一，路径指向本包）：
//
//	go build -ldflags "\
//	  -X github.com/sidchai/compkg/pkg/buildinfo.GitCommit=$(git rev-parse --short=8 HEAD) \
//	  -X github.com/sidchai/compkg/pkg/buildinfo.BuildTime=$(date -u +%Y%m%dT%H%M%SZ) \
//	  -X github.com/sidchai/compkg/pkg/buildinfo.Version=v1.2.3"
//
// 未注入时（如 go run 本地调试）保持 "unknown"，BuildID 仍可正常拼接，便于排查。
package buildinfo

import "fmt"

// 以下变量由编译期 -ldflags -X 注入。禁止改成常量，否则 ldflags 注入失效。
var (
	// GitCommit 本次构建对应的 git 短 commit（建议 8 位）。
	GitCommit = "unknown"
	// BuildTime 构建时刻，UTC，格式 20060102T150405Z。
	BuildTime = "unknown"
	// Version 语义化版本号（可选，发版打 tag 时注入）。
	Version = "dev"
)

// BuildID 返回唯一标识本次构建的指纹字符串：{GitCommit}@{BuildTime}。
//
// 该值会随进程心跳上报到 Redis，供发版检测工具比对"运行版本 == 预期版本"。
func BuildID() string {
	return fmt.Sprintf("%s@%s", GitCommit, BuildTime)
}

// Summary 返回人类可读的完整版本摘要，用于 --version 输出与日志。
func Summary() string {
	return fmt.Sprintf("version=%s commit=%s buildTime=%s", Version, GitCommit, BuildTime)
}
