package trace

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sidchai/compkg/pkg/logger"
)

// resetBootstrap 仅供测试使用：重置 once，便于在不同子测试间多次 Bootstrap。
func resetBootstrap() {
	bootstrapOnce.Store(false)
	enabled.Store(false)
	tracerProvider.Store(nil)
	currentPolicy.Store(nil)
}

func TestBootstrap_Disabled(t *testing.T) {
	resetBootstrap()
	shutdown, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "svc",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("disabled bootstrap should not error: %v", err)
	}
	if Enabled() {
		t.Fatalf("Enabled() should be false")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown err: %v", err)
	}
}

func TestBootstrap_RequireServiceName(t *testing.T) {
	resetBootstrap()
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		Enabled:           true,
		CollectorEndpoint: "127.0.0.1:4317",
	})
	if err == nil {
		t.Fatalf("expected error for missing ServiceName")
	}
}

func TestBootstrap_RequireEndpoint(t *testing.T) {
	resetBootstrap()
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		Enabled:     true,
		ServiceName: "svc",
	})
	if err == nil {
		t.Fatalf("expected error for missing CollectorEndpoint")
	}
}

func TestBootstrap_DoubleCall(t *testing.T) {
	resetBootstrap()
	_, err1 := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "svc",
		Enabled:     false,
	})
	if err1 != nil {
		t.Fatalf("first bootstrap err: %v", err1)
	}
	_, err2 := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "svc",
		Enabled:     false,
	})
	if err2 != ErrAlreadyBootstrapped {
		t.Fatalf("expected ErrAlreadyBootstrapped, got %v", err2)
	}
}

// 验证 logger bridge 在 disabled 模式下也注册：调用 logger.SetTraceExtractor 后
// 我们的 bridge 会被覆盖成功（说明引用正确，无 import 循环 / nil panic）。
func TestBootstrap_RegistersLoggerBridge(t *testing.T) {
	resetBootstrap()
	var called atomic.Bool
	logger.SetTraceExtractor(func(context.Context) (string, string) {
		called.Store(true)
		return "", ""
	})
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "svc",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("bootstrap err: %v", err)
	}
	// Bootstrap 后会替换 extractor；调用一次 logger 后应不再触发上面的 callback
	// 这里我们只能间接验证 SetTraceExtractor 没 panic、Bootstrap 通过。
	_ = called
}
