package logger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 在临时目录建一个文件 sink 并 Bootstrap，返回日志文件路径。
func bootstrapForTest(t *testing.T, opts BootstrapOptions) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if opts.ServiceName == "" {
		opts.ServiceName = "test-svc"
	}
	if opts.Env == "" {
		opts.Env = "test"
	}
	opts.File.Path = path
	opts.EnableStdout = false
	require.NoError(t, Bootstrap(opts))
	t.Cleanup(func() {
		_ = Shutdown(context.Background())
	})
	return path
}

// 读 ndjson 行返回 map slice。
func readLogLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	require.NoError(t, Sync())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		out = append(out, m)
	}
	return out
}

// 基础：Entry 结构化字段 + 固定 service/env。
func TestLogger_EntryBasic(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{Version: "1.2.3"})
	Ctx(context.Background()).
		With("port", 8080).
		With("addr", "127.0.0.1").
		Info("server started")

	lines := readLogLines(t, path)
	require.Len(t, lines, 1)
	l := lines[0]
	assert.Equal(t, "server started", l["msg"])
	assert.Equal(t, "info", l["level"])
	assert.Equal(t, "test-svc", l["service"])
	assert.Equal(t, "test", l["env"])
	assert.Equal(t, "1.2.3", l["version"])
	assert.EqualValues(t, 8080, l["port"])
	assert.Equal(t, "127.0.0.1", l["addr"])
}

// BizCtx：device_no / account_id / msg_id 自动注入。
func TestLogger_BizCtxInjection(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{})
	ctx := WithBizCtx(context.Background(), BizCtxKeys{
		DeviceNo:  "806599A1B2C3",
		AccountId: 12345,
		MsgId:     "msg-abc",
	})
	Ctx(ctx).Info("processing")

	l := readLogLines(t, path)[0]
	assert.Equal(t, "806599A1B2C3", l["device_no"])
	assert.EqualValues(t, 12345, l["account_id"])
	assert.Equal(t, "msg-abc", l["msg_id"])
}

// 业务字段二次绑定不应清空已有字段（增量补充）。
func TestLogger_BizCtxIncremental(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{})
	ctx := WithBizCtx(context.Background(), BizCtxKeys{DeviceNo: "D1"})
	ctx = WithBizCtx(ctx, BizCtxKeys{AccountId: 999})
	Ctx(ctx).Info("ok")

	l := readLogLines(t, path)[0]
	assert.Equal(t, "D1", l["device_no"])
	assert.EqualValues(t, 999, l["account_id"])
}

// trace 抽取器注入 + 写日志。
func TestLogger_TraceInjection(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{})
	type traceKey struct{}
	SetTraceExtractor(func(ctx context.Context) (string, string) {
		v, _ := ctx.Value(traceKey{}).(string)
		return v, "span-1"
	})
	t.Cleanup(func() {
		SetTraceExtractor(func(context.Context) (string, string) { return "", "" })
	})
	ctx := context.WithValue(context.Background(), traceKey{}, "trace-xyz")
	Ctx(ctx).Info("traced")

	l := readLogLines(t, path)[0]
	assert.Equal(t, "trace-xyz", l["trace_id"])
	assert.Equal(t, "span-1", l["span_id"])
}

// 脱敏：password 字段 *** ；message 中手机号被掩码。
func TestLogger_Sanitize(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{})
	Ctx(context.Background()).
		With("password", "real-secret").
		With("phone", "13812345678").
		Info("user 13987654321 login from 192.168.0.1")

	l := readLogLines(t, path)[0]
	assert.Equal(t, "***", l["password"])
	assert.Equal(t, "138****5678", l["phone"])
	assert.Contains(t, l["msg"].(string), "139****4321")
	assert.NotContains(t, l["msg"].(string), "13987654321")
}

// 关闭脱敏。
func TestLogger_DisableSanitize(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{SanitizeRules: DisableSanitize})
	Ctx(context.Background()).With("password", "raw").Info("phone=13812345678")
	l := readLogLines(t, path)[0]
	assert.Equal(t, "raw", l["password"])
	assert.Contains(t, l["msg"].(string), "13812345678")
}

// 级别热更新：默认 Info；切到 Warn 后 Info 不再输出。
func TestLogger_DynamicLevel(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{Level: LevelInfo})
	Ctx(context.Background()).Info("first")
	SetLevelDynamic(LevelWarn)
	Ctx(context.Background()).Info("second-suppressed")
	Ctx(context.Background()).Warn("third")

	lines := readLogLines(t, path)
	require.Len(t, lines, 2)
	assert.Equal(t, "first", lines[0]["msg"])
	assert.Equal(t, "third", lines[1]["msg"])
}

// 采样：Info 每 3 条放行 1 条；Warn 全部放行。
func TestLogger_Sampling(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{SampleEveryN: 3})
	for i := 0; i < 9; i++ {
		Ctx(context.Background()).Info("info-msg")
	}
	for i := 0; i < 3; i++ {
		Ctx(context.Background()).Warn("warn-msg")
	}
	lines := readLogLines(t, path)
	infos, warns := 0, 0
	for _, l := range lines {
		switch l["level"] {
		case "info":
			infos++
		case "warn":
			warns++
		}
	}
	// 9 / 3 = 3 条 info，3 条 warn。
	assert.Equal(t, 3, infos, "expected sampled 1/3 of 9 Info logs")
	assert.Equal(t, 3, warns, "Warn must not be sampled")
}

// hlog 适配：CtxInfof 应写入 trace + biz 字段。
func TestLogger_HlogAdapter(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{})
	hlog.SetLogger(HlogAdapter())

	ctx := WithBizCtx(context.Background(), BizCtxKeys{DeviceNo: "D9"})
	hlog.CtxInfof(ctx, "device %s online port=%d", "D9", 8080)
	hlog.CtxErrorf(ctx, "fail %s", "boom")

	lines := readLogLines(t, path)
	require.Len(t, lines, 2)
	assert.Equal(t, "info", lines[0]["level"])
	assert.Equal(t, "device D9 online port=8080", lines[0]["msg"])
	assert.Equal(t, "D9", lines[0]["device_no"])
	assert.Equal(t, "error", lines[1]["level"])
	assert.Equal(t, "fail boom", lines[1]["msg"])
}

// hlog SetLevel 应反映到全局 driver。
func TestLogger_HlogSetLevel(t *testing.T) {
	path := bootstrapForTest(t, BootstrapOptions{Level: LevelDebug})
	hlog.SetLogger(HlogAdapter())
	hlog.Debugf("d1")
	hlog.SetLevel(hlog.LevelWarn)
	hlog.Debugf("d2-suppressed")
	hlog.Infof("i-suppressed")
	hlog.Warnf("w1")

	lines := readLogLines(t, path)
	msgs := []string{}
	for _, l := range lines {
		msgs = append(msgs, l["msg"].(string))
	}
	assert.Equal(t, []string{"d1", "w1"}, msgs)
}

// ParseLevel 各别名。
func TestLogger_ParseLevel(t *testing.T) {
	assert.Equal(t, LevelDebug, ParseLevel("debug"))
	assert.Equal(t, LevelInfo, ParseLevel("info"))
	assert.Equal(t, LevelWarn, ParseLevel("warning"))
	assert.Equal(t, LevelError, ParseLevel("err"))
	assert.Equal(t, LevelInfo, ParseLevel("unknown"))
}
