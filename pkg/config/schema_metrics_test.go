package config

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 基础 schema：要求 server.port 为 int 在 [1024, 65535]；server.name 为字符串；
// 二者都必填。
const basicSchemaJSON = `{
  "type": "object",
  "properties": {
    "server": {
      "type": "object",
      "properties": {
        "port": {"type": "integer", "minimum": 1024, "maximum": 65535},
        "name": {"type": "string", "minLength": 1}
      },
      "required": ["port", "name"]
    }
  },
  "required": ["server"]
}`

// 启动校验通过：配置合规。
func TestSchema_StartupPass(t *testing.T) {
	path := writeTempYaml(t, "server:\n  name: svc\n  port: 13007\n")
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-pass",
		LocalPath:   path,
		Schema:      []byte(basicSchemaJSON),
	})
	require.NoError(t, err)
	assert.Equal(t, 13007, l.GetInt("server.port"))
}

// 启动校验失败：缺必填字段 -> 拒绝启动，且验证失败指标 +1。
func TestSchema_StartupReject_MissingRequired(t *testing.T) {
	before := readCounterValue(t, "config_validation_failure_total", map[string]string{"service": "schema-reject-1"})

	path := writeTempYaml(t, "server:\n  port: 13007\n") // 缺 name
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-reject-1",
		LocalPath:   path,
		Schema:      []byte(basicSchemaJSON),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation failed")

	after := readCounterValue(t, "config_validation_failure_total", map[string]string{"service": "schema-reject-1"})
	assert.Equal(t, before+1, after, "validation failure metric should increase")
}

// 启动校验失败：字段类型错（port 是 string）。
func TestSchema_StartupReject_WrongType(t *testing.T) {
	path := writeTempYaml(t, "server:\n  name: svc\n  port: \"not-an-int\"\n")
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-reject-2",
		LocalPath:   path,
		Schema:      []byte(basicSchemaJSON),
	})
	require.Error(t, err)
}

// 启动校验失败：值越界（port=80 < 1024）。
func TestSchema_StartupReject_OutOfRange(t *testing.T) {
	path := writeTempYaml(t, "server:\n  name: svc\n  port: 80\n")
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-reject-3",
		LocalPath:   path,
		Schema:      []byte(basicSchemaJSON),
	})
	require.Error(t, err)
}

// 热更失败：远端推来一份违反 schema 的配置，旧值保留，listener 不触发。
func TestSchema_HotReloadReject_KeepsOldConfig(t *testing.T) {
	// 本地有合规 port=13007 + name=svc；mockSource 的 Fetch 返回 port=8000，
	// 但没有 name，会与本地合并后仍合规（name=svc 来自本地）。
	path := writeTempYaml(t, "server:\n  name: svc\n  port: 13007\n")
	src := &mockSource{}
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-hot-reject",
		LocalPath:   path,
		Remote:      src,
		Schema:      []byte(basicSchemaJSON),
	})
	require.NoError(t, err)
	defer l.Close()

	// Bootstrap 后 port 已经被 remote override 成 8000。
	require.Equal(t, 8000, l.GetInt("server.port"))

	var called atomic.Int32
	l.OnChange("server", func(_, _ any) { called.Add(1) })

	before := readCounterValue(t, "config_validation_failure_total", map[string]string{"service": "schema-hot-reject"})

	// 推一份非法配置：port 超上限
	src.onChange("service.demo.yaml", []byte("server:\n  name: svc\n  port: 999999\n"))

	// 旧配置应保留（仍是 8000，没被非法值覆盖）
	assert.Equal(t, 8000, l.GetInt("server.port"), "invalid hot reload must keep previous value")
	// listener 绝对不应被触发
	assert.Equal(t, int32(0), called.Load(), "listener must not fire on schema failure")
	// 失败指标 +1
	after := readCounterValue(t, "config_validation_failure_total", map[string]string{"service": "schema-hot-reject"})
	assert.Equal(t, before+1, after)
}

// 热更成功：新配置合规 -> listener 触发 + listen_total +1。
func TestSchema_HotReloadPass_ListenMetricInc(t *testing.T) {
	path := writeTempYaml(t, "server:\n  name: svc\n  port: 13007\n")
	src := &mockSource{}
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName:   "schema-hot-pass",
		LocalPath:     path,
		Remote:        src,
		Schema:        []byte(basicSchemaJSON),
		HotReloadable: []string{"server"},
	})
	require.NoError(t, err)
	defer l.Close()

	var called atomic.Int32
	l.OnChange("server", func(_, _ any) { called.Add(1) })

	before := readCounterValue(t, "config_listen_total",
		map[string]string{"service": "schema-hot-pass", "data_id": "service.demo.yaml"})

	src.onChange("service.demo.yaml", []byte("server:\n  name: svc\n  port: 20000\n"))

	assert.Equal(t, int32(1), called.Load())
	assert.Equal(t, 20000, l.GetInt("server.port"))

	after := readCounterValue(t, "config_listen_total",
		map[string]string{"service": "schema-hot-pass", "data_id": "service.demo.yaml"})
	assert.Equal(t, before+1, after)
}

// 无 Schema 时行为保持兼容（向后兼容）：全流程不校验，不计指标。
func TestSchema_NotProvided_Compat(t *testing.T) {
	path := writeTempYaml(t, "anything: goes\n")
	l, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-omitted",
		LocalPath:   path,
	})
	require.NoError(t, err)
	defer l.Close()
	assert.Equal(t, "goes", l.GetString("anything"))
}

// 编译期非法 Schema：注册时就应报错，而不是跑一半才失败。
func TestSchema_InvalidSchemaDoc(t *testing.T) {
	path := writeTempYaml(t, "k: v\n")
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "schema-broken",
		LocalPath:   path,
		Schema:      []byte(`{"type": 123}`), // type 应为 string
	})
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "compile schema") || strings.Contains(err.Error(), "schema"),
		"expect schema compile error, got: %v", err)
}

// ---- 指标 fetch_total / nacos_disconnect / local_fallback ----

// 远端 fetch 成功：fetch_total{result=ok} +1。
func TestMetrics_FetchOk(t *testing.T) {
	before := readCounterValue(t, "config_fetch_total",
		map[string]string{"service": "metrics-ok", "source": "mock", "result": "ok"})

	path := writeTempYaml(t, "a: b\n")
	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "metrics-ok",
		LocalPath:   path,
		Remote:      &mockSource{},
	})
	require.NoError(t, err)

	after := readCounterValue(t, "config_fetch_total",
		map[string]string{"service": "metrics-ok", "source": "mock", "result": "ok"})
	assert.Equal(t, before+1, after)
}

// 远端 fetch 失败：fetch_total{result=error}+1、nacos_disconnect+1、local_fallback_used+1。
func TestMetrics_FetchError(t *testing.T) {
	path := writeTempYaml(t, "a: b\n")

	beforeErr := readCounterValue(t, "config_fetch_total",
		map[string]string{"service": "metrics-err", "source": "failing", "result": "error"})
	beforeDisc := readCounterValueNoLabel(t, "config_nacos_disconnect_total")
	beforeFb := readCounterValue(t, "config_local_fallback_used_total",
		map[string]string{"service": "metrics-err"})

	_, err := Bootstrap(context.Background(), BootstrapOptions{
		ServiceName: "metrics-err",
		LocalPath:   path,
		Remote:      &failingSource{},
	})
	require.NoError(t, err, "Bootstrap must succeed by falling back to local")

	assert.Equal(t, beforeErr+1, readCounterValue(t, "config_fetch_total",
		map[string]string{"service": "metrics-err", "source": "failing", "result": "error"}))
	assert.Equal(t, beforeDisc+1, readCounterValueNoLabel(t, "config_nacos_disconnect_total"))
	assert.Equal(t, beforeFb+1, readCounterValue(t, "config_local_fallback_used_total",
		map[string]string{"service": "metrics-err"}))
}

// failingSource 始终返回 Fetch 错误，用于验证失败路径指标。
type failingSource struct{}

func (f *failingSource) Name() string { return "failing" }
func (f *failingSource) Fetch(context.Context) (map[string][]byte, error) {
	return nil, assertErr("simulated remote down")
}
func (f *failingSource) Listen(context.Context, func(string, []byte)) (func() error, error) {
	return func() error { return nil }, nil
}

type assertErr string

func (a assertErr) Error() string { return string(a) }

// ---- 指标读取工具 ----

// readCounterValue 从 DefaultGatherer 读指定 metric 和 label set 的 counter 值，缺失返回 0。
func readCounterValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := MetricsGatherer().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func readCounterValueNoLabel(t *testing.T, metricName string) float64 {
	t.Helper()
	mfs, err := MetricsGatherer().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func matchLabels(m *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(m.Label))
	for _, lp := range m.Label {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
