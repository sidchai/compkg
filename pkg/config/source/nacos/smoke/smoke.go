// 冒烟测试程序：连接本地 Nacos 验证 Loader+Source+OnChange 端到端流程。
//
// 运行（PowerShell）：
//
//	$env:NACOS_USERNAME="nacos"; $env:NACOS_PASSWORD="xxxx"; go run ./pkg/config/source/nacos/smoke
//
// 不设环境变量时默认 nacos / nacos。
//
// 步骤：
//  1. 用 nacos-sdk-go 直接发布一份测试配置到 dataId=compkg.smoke.yaml
//  2. config.Bootstrap + Nacos source 拉取，校验值
//  3. 注册 OnChange listener
//  4. 再次 PublishConfig 修改值，等待 listener 回调
//  5. 校验远端值已生效
//  6. 删除 dataId 清理现场
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	nacosvo "github.com/nacos-group/nacos-sdk-go/v2/vo"

	cfgsdk "github.com/sidchai/compkg/pkg/config"
	nacossrc "github.com/sidchai/compkg/pkg/config/source/nacos"
)

const (
	nacosAddr = "127.0.0.1:8848"
	group     = "DEFAULT_GROUP"
	dataId    = "compkg.smoke.yaml"

	initialYaml = `feature:
  greeting: "hello-v1"
  threshold: 100
`
	updatedYaml = `feature:
  greeting: "hello-v2"
  threshold: 999
`
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("[FAIL] %v", err)
	}
	fmt.Println("\n[PASS] All Nacos smoke checks passed.")
}

func run() error {
	username, password := credsFromEnv()

	// ---- 准备：构造原生 Nacos 客户端用于 publish/delete ----
	src, err := nacossrc.New(nacossrc.Options{
		Servers:   []string{nacosAddr},
		Group:     group,
		DataIds:   []string{dataId},
		Username:  username,
		Password:  password,
		LogLevel:  "warn",
		LogDir:    os.TempDir() + "/compkg-smoke-log",
		CacheDir:  os.TempDir() + "/compkg-smoke-cache",
		TimeoutMs: 5000,
	})
	if err != nil {
		return fmt.Errorf("new source: %w", err)
	}

	rawCli, err := newRawClient(username, password)
	if err != nil {
		return fmt.Errorf("new raw client: %w", err)
	}

	// 清理旧值并发布初始配置
	step("publish initial config")
	ok, err := rawCli.PublishConfig(nacosvo.ConfigParam{
		DataId:  dataId,
		Group:   group,
		Content: initialYaml,
		Type:    "yaml",
	})
	if err != nil || !ok {
		return fmt.Errorf("publish initial: ok=%v err=%v", ok, err)
	}
	if os.Getenv("KEEP_CONFIG") != "1" {
		defer func() {
			_, _ = rawCli.DeleteConfig(nacosvo.ConfigParam{DataId: dataId, Group: group})
		}()
	} else {
		fmt.Println("    -> KEEP_CONFIG=1: 保留 dataId 供 Nacos 控制台查看")
	}

	// 等 Nacos 落库（避免 Bootstrap 拉到空）
	time.Sleep(800 * time.Millisecond)

	// ---- Bootstrap ----
	step("bootstrap with local fallback + nacos source")
	localPath := writeLocalFallback()
	defer os.Remove(localPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loader, err := cfgsdk.Bootstrap(ctx, cfgsdk.BootstrapOptions{
		ServiceName: "compkg.smoke",
		Namespace:   "smoke",
		LocalPath:   localPath,
		Remote:      src,
		HotReloadable: []string{
			"feature",
		},
		FetchTimeout: 5 * time.Second,
		Logger: func(level, msg string, kv ...any) {
			log.Printf("[%s] %s %v", level, msg, kv)
		},
	})
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	defer loader.Close()

	// ---- 校验初始值 ----
	step("assert initial value")
	if got := loader.GetString("feature.greeting"); got != "hello-v1" {
		return fmt.Errorf("feature.greeting want=hello-v1 got=%q", got)
	}
	if got := loader.GetInt("feature.threshold"); got != 100 {
		return fmt.Errorf("feature.threshold want=100 got=%d", got)
	}
	fmt.Println("    -> greeting=hello-v1 threshold=100 ✓")

	// ---- 注册 OnChange ----
	step("register OnChange listener")
	var (
		mu     sync.Mutex
		fired  bool
		newVal any
		notify = make(chan struct{}, 1)
	)
	loader.OnChange("feature", func(_, newCfg any) {
		mu.Lock()
		fired = true
		newVal = newCfg
		mu.Unlock()
		select {
		case notify <- struct{}{}:
		default:
		}
	})

	// ---- 修改远端配置触发回调 ----
	step("publish updated config")
	ok, err = rawCli.PublishConfig(nacosvo.ConfigParam{
		DataId:  dataId,
		Group:   group,
		Content: updatedYaml,
		Type:    "yaml",
	})
	if err != nil || !ok {
		return fmt.Errorf("publish update: ok=%v err=%v", ok, err)
	}

	step("waiting for OnChange callback (max 10s)")
	select {
	case <-notify:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("OnChange listener not fired within 10s")
	}

	mu.Lock()
	defer mu.Unlock()
	if !fired {
		return fmt.Errorf("listener flag not set")
	}
	fmt.Printf("    -> listener fired, newVal type=%T\n", newVal)

	// ---- 校验更新后值 ----
	step("assert updated value")
	if got := loader.GetString("feature.greeting"); got != "hello-v2" {
		return fmt.Errorf("after update: greeting want=hello-v2 got=%q", got)
	}
	if got := loader.GetInt("feature.threshold"); got != 999 {
		return fmt.Errorf("after update: threshold want=999 got=%d", got)
	}
	fmt.Println("    -> greeting=hello-v2 threshold=999 ✓")

	return nil
}

func step(s string) {
	fmt.Printf("\n>>> %s\n", s)
}

// credsFromEnv 读取 NACOS_USERNAME / NACOS_PASSWORD，缺省 nacos/nacos。
func credsFromEnv() (string, string) {
	u := os.Getenv("NACOS_USERNAME")
	p := os.Getenv("NACOS_PASSWORD")
	if u == "" {
		u = "nacos"
	}
	if p == "" {
		p = "nacos"
	}
	return u, p
}

// newRawClient 直连 Nacos 的原生 ConfigClient，用于 Publish/Delete。
func newRawClient(username, password string) (config_client.IConfigClient, error) {
	sc := []constant.ServerConfig{*constant.NewServerConfig("127.0.0.1", 8848)}
	cc := *constant.NewClientConfig(
		constant.WithUsername(username),
		constant.WithPassword(password),
		constant.WithTimeoutMs(5000),
		constant.WithLogLevel("warn"),
		constant.WithLogDir(os.TempDir()+"/compkg-smoke-raw-log"),
		constant.WithCacheDir(os.TempDir()+"/compkg-smoke-raw-cache"),
		constant.WithNotLoadCacheAtStart(true),
	)
	return clients.NewConfigClient(nacosvo.NacosClientParam{
		ClientConfig:  &cc,
		ServerConfigs: sc,
	})
}

// writeLocalFallback 写一个最小本地 yaml（带 server.name 满足 ServiceName 要求即可）。
func writeLocalFallback() string {
	f, err := os.CreateTemp("", "compkg-smoke-*.yaml")
	if err != nil {
		log.Fatalf("temp file: %v", err)
	}
	_, _ = f.WriteString("placeholder: true\n")
	_ = f.Close()
	return f.Name()
}
