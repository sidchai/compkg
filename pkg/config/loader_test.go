package config

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sidchai/compkg/pkg/encrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempYaml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// 基础读取 + dot-key + 类型转换。
func TestLoader_LocalBasic(t *testing.T) {
	yaml := `
server:
  name: test-svc
  port: 13007
  enabled: true
  tags: [a, b, c]
nested:
  redis:
    addr: 127.0.0.1:6379
`
	path := writeTempYaml(t, yaml)
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "unit",
		LocalPath:   path,
	})
	require.NoError(t, err)
	defer l.Close()

	assert.Equal(t, "test-svc", l.GetString("server.name"))
	assert.Equal(t, 13007, l.GetInt("server.port"))
	assert.True(t, l.GetBool("server.enabled"))
	assert.Equal(t, []string{"a", "b", "c"}, l.GetStringSlice("server.tags"))
	assert.Equal(t, "127.0.0.1:6379", l.GetString("nested.redis.addr"))
}

// Bind 子树到 struct。
func TestLoader_Bind(t *testing.T) {
	yaml := `
server:
  port: 9000
  tcp_timeout: 10s
`
	path := writeTempYaml(t, yaml)
	l, err := Bootstrap(context.Background(), BootstrapOptions{LocalPath: path})
	require.NoError(t, err)

	type ServerCfg struct {
		Port       int           `mapstructure:"port"`
		TcpTimeout time.Duration `mapstructure:"tcp_timeout"`
	}
	var cfg ServerCfg
	require.NoError(t, l.Bind("server", &cfg))
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 10*time.Second, cfg.TcpTimeout)
}

// 环境变量白名单展开。
func TestLoader_EnvExpand(t *testing.T) {
	t.Setenv("POD_IP", "10.1.2.3")
	t.Setenv("DB_PASSWORD", "should-not-leak") // 不在白名单
	yaml := `
server:
  address: ${POD_IP}
  secret: ${DB_PASSWORD}
`
	path := writeTempYaml(t, yaml)
	l, err := Bootstrap(context.Background(), BootstrapOptions{LocalPath: path})
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", l.GetString("server.address"))
	// 不在白名单的占位保留原样，避免误展开机密。
	assert.Equal(t, "${DB_PASSWORD}", l.GetString("server.secret"))
}

// ENC 解密：密钥从配置中读取（默认 path config.encryptKey）。
func TestLoader_Decrypt(t *testing.T) {
	rawKey := make([]byte, 16)
	for i := range rawKey {
		rawKey[i] = byte(i + 1)
	}
	keyStr := base64.StdEncoding.EncodeToString(rawKey)

	cipher, err := encrypt.EncryptConfigValue(rawKey, "real-password")
	require.NoError(t, err)

	yaml := "config:\n  encryptKey: " + keyStr + "\nredis:\n  password: " + cipher + "\n"
	path := writeTempYaml(t, yaml)

	l, err := Bootstrap(context.Background(), BootstrapOptions{LocalPath: path})
	require.NoError(t, err)
	assert.Equal(t, "real-password", l.GetString("redis.password"))
}

// 多密钥轮换：第一个密钥不匹配，第二个能解开仍正常启动。
func TestLoader_DecryptMultiKeyRotation(t *testing.T) {
	wrongKey := make([]byte, 16)
	for i := range wrongKey {
		wrongKey[i] = 0xAA
	}
	rightKey := make([]byte, 16)
	for i := range rightKey {
		rightKey[i] = byte(i + 1)
	}
	cipher, err := encrypt.EncryptConfigValue(rightKey, "secret-v2")
	require.NoError(t, err)

	yaml := "config:\n" +
		"  encryptKey:\n" +
		"    - " + base64.StdEncoding.EncodeToString(wrongKey) + "\n" +
		"    - " + base64.StdEncoding.EncodeToString(rightKey) + "\n" +
		"db:\n  password: " + cipher + "\n"
	path := writeTempYaml(t, yaml)

	l, err := Bootstrap(context.Background(), BootstrapOptions{LocalPath: path})
	require.NoError(t, err)
	assert.Equal(t, "secret-v2", l.GetString("db.password"))
}

// 配置中含 ENC 但未提供密钥应直接报错。
func TestLoader_DecryptMissingKey(t *testing.T) {
	yaml := "redis:\n  password: ENC(deadbeef)\n"
	path := writeTempYaml(t, yaml)
	_, err := Bootstrap(context.Background(), BootstrapOptions{LocalPath: path})
	require.Error(t, err)
}

// 自定义 EncryptKeyPath：从非默认路径读密钥。
func TestLoader_DecryptCustomKeyPath(t *testing.T) {
	rawKey := make([]byte, 16)
	for i := range rawKey {
		rawKey[i] = byte(i + 1)
	}
	cipher, err := encrypt.EncryptConfigValue(rawKey, "x")
	require.NoError(t, err)
	yaml := "secrets:\n  master: " + base64.StdEncoding.EncodeToString(rawKey) + "\nfoo: " + cipher + "\n"
	path := writeTempYaml(t, yaml)
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		LocalPath:      path,
		EncryptKeyPath: "secrets.master",
	})
	require.NoError(t, err)
	assert.Equal(t, "x", l.GetString("foo"))
}

// Feature flag 灰度。
func TestLoader_Feature(t *testing.T) {
	yaml := `
flags:
  on_full:
    enabled: true
    rollout: 100
  off:
    enabled: false
    rollout: 100
  half:
    enabled: true
    rollout: 50
`
	path := writeTempYaml(t, yaml)
	l, err := Bootstrap(context.Background(), BootstrapOptions{LocalPath: path})
	require.NoError(t, err)

	assert.True(t, l.Feature("on_full", FeatureContext{DeviceNo: "any"}))
	assert.False(t, l.Feature("off", FeatureContext{DeviceNo: "any"}))
	assert.False(t, l.Feature("missing", FeatureContext{}))

	// 50% 灰度：检查多个 deviceNo 大致一半命中（CRC32 分布近均匀）。
	hit := 0
	for i := 0; i < 200; i++ {
		ctx := FeatureContext{DeviceNo: string(rune('A'+i%26)) + string(rune('0'+i%10))}
		if l.Feature("half", ctx) {
			hit++
		}
	}
	assert.Greater(t, hit, 60)
	assert.Less(t, hit, 140)
}

// 远程 source mock：触发 OnChange listener。
type mockSource struct {
	onChange func(string, []byte)
}

func (m *mockSource) Name() string { return "mock" }
func (m *mockSource) Fetch(ctx context.Context) (map[string][]byte, error) {
	return map[string][]byte{
		"service.demo.yaml": []byte("server:\n  port: 8000\n"),
	}, nil
}
func (m *mockSource) Listen(_ context.Context, on func(string, []byte)) (func() error, error) {
	m.onChange = on
	return func() error { return nil }, nil
}

func TestLoader_RemoteOverrideAndOnChange(t *testing.T) {
	yaml := "server:\n  port: 1000\n  name: local\n"
	path := writeTempYaml(t, yaml)

	src := &mockSource{}
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		LocalPath: path,
		Remote:    src,
	})
	require.NoError(t, err)
	defer l.Close()

	// 远端 override port=8000，本地 name 仍保留。
	assert.Equal(t, 8000, l.GetInt("server.port"))
	assert.Equal(t, "local", l.GetString("server.name"))

	var called atomic.Int32
	var newVal atomic.Value
	l.OnChange("server.port", func(old, n any) {
		called.Add(1)
		newVal.Store(n)
	})

	// 模拟远端推送新配置。
	src.onChange("service.demo.yaml", []byte("server:\n  port: 9999\n"))

	// listener 是同步触发的，确认。
	require.Equal(t, int32(1), called.Load())
	assert.Equal(t, 9999, l.GetInt("server.port"))
}

// HotReloadable 白名单：未列入的 key 变更不应触发 listener。
func TestLoader_HotReloadGuard(t *testing.T) {
	path := writeTempYaml(t, "server:\n  port: 1000\n  name: x\n")
	src := &mockSource{}
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		LocalPath:     path,
		Remote:        src,
		HotReloadable: []string{"server.port"}, // 仅 port 允许热更
	})
	require.NoError(t, err)

	var portCalls, nameCalls atomic.Int32
	l.OnChange("server.port", func(old, n any) { portCalls.Add(1) })
	l.OnChange("server.name", func(old, n any) { nameCalls.Add(1) })

	src.onChange("svc.yaml", []byte("server:\n  port: 2000\n  name: y\n"))
	assert.Equal(t, int32(1), portCalls.Load())
	assert.Equal(t, int32(0), nameCalls.Load(), "non-hot-reloadable key should not fire listener")
	assert.Equal(t, 2000, l.GetInt("server.port"))
	// 即使不触发 listener，配置值仍已更新。
	assert.Equal(t, "y", l.GetString("server.name"))
}
