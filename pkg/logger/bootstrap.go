package logger

import (
	"context"
	"errors"
	"sync/atomic"
)

// 全局 driver。Bootstrap 调用一次后被原子设置。
var globalDriver atomic.Pointer[driver]

// Bootstrap 初始化全局日志驱动（zap + lumberjack）。
//
// 推荐顺序：
//  1. 配置中心 Bootstrap
//  2. logger.Bootstrap(...)
//  3. hlog.SetLogger(logger.HlogAdapter())  // 如使用 hertz/hlog
//  4. 业务启动
//
// 多次调用：后一次会替换，但旧 driver 的文件句柄不会被关闭（避免在切换瞬间丢日志）。
// 业务侧若需要彻底关闭，请显式 Shutdown 旧实例。
func Bootstrap(opts BootstrapOptions) error {
	d, err := buildDriver(opts)
	if err != nil {
		return err
	}
	globalDriver.Store(d)
	return nil
}

// getDriver 返回当前全局 driver；未 Bootstrap 时返回 nil。
func getDriver() *driver {
	return globalDriver.Load()
}

// Sync 刷写所有日志缓冲。建议在 main defer 中调用。
func Sync() error {
	d := getDriver()
	if d == nil {
		return nil
	}
	return d.sync()
}

// Shutdown 关闭文件等资源；ctx 仅用于未来对接远程 sink 异步 flush。
func Shutdown(ctx context.Context) error {
	d := getDriver()
	if d == nil {
		return nil
	}
	return d.shutdown()
}

// SetLevelDynamic 运行时切换日志级别。建议绑定到 RFC-06 配置中心 OnChange("log.level")。
func SetLevelDynamic(lv Level) {
	if d := getDriver(); d != nil {
		d.setLevel(lv)
	}
}

// errNotBootstrapped 仅供测试使用。
var errNotBootstrapped = errors.New("logger: not bootstrapped")
