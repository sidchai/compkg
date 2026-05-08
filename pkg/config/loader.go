package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sidchai/compkg/pkg/encrypt"
	"github.com/spf13/viper"
)

// Loader 全局配置加载器。所有读操作并发安全。
//
// 内部以 *viper.Viper 作为权威存储，支持 dot-key（"server.master_tcp_port"）。
// 远程更新到达时，重新构建一个 Viper 替换 atomic 指针，旧的 Viper 仍可被读路径安全使用至 GC。
type Loader struct {
	opts BootstrapOptions

	// vp 使用 atomic.Pointer 保证读写不互相阻塞。
	vp atomic.Pointer[viper.Viper]

	// remoteCancel 关闭远程订阅。
	remoteCancel func() error

	// listeners key -> []callback；key 为 dot-key 前缀，子树任何变更都会触发。
	mu        sync.RWMutex
	listeners map[string][]func(old, new any)

	// envWhitelist 集合形式的环境变量白名单。
	envWhitelist map[string]struct{}

	// hotReload set 形式的可热更新前缀；nil 表示全部允许。
	hotReload map[string]struct{}

	// remoteRaw 远端最近一次抓到的 raw bytes 缓存（按 dataId），便于变更时重组。
	remoteMu  sync.Mutex
	remoteRaw map[string][]byte

	// schema 编译好的 JSON Schema（可为 nil，表示不校验）。
	schema *jsonschema.Schema
}

// 默认环境变量白名单。
var defaultEnvWhitelist = []string{
	"POD_IP",
	"HOSTNAME",
	"VERSION",
	"NODE_NAME",
	"NAMESPACE",
}

// Bootstrap 初始化 Loader。
//
//   - 必填：opts.LocalPath
//   - opts.Remote 为 nil 时仅本地，无热更新
//   - 失败语义：本地解析失败 -> 返回 error；Remote 不可达 -> WARN 不阻塞；ENC 解密失败 -> 返回 error；
//     Schema 校验暂未集成（Phase 2 留口）。
func Bootstrap(ctx context.Context, opts BootstrapOptions) (*Loader, error) {
	if opts.LocalPath == "" {
		return nil, errors.New("config: LocalPath is required")
	}
	if opts.LocalConfigType == "" {
		opts.LocalConfigType = "yaml"
	}
	if opts.FetchTimeout <= 0 {
		opts.FetchTimeout = 5 * time.Second
	}

	if opts.EncryptKeyPath == "" {
		opts.EncryptKeyPath = "config.encryptKey"
	}

	// 预编译 Schema（失败直接拒绝启动，避免跑起来再爆）。
	schema, err := compileSchema(opts.Schema)
	if err != nil {
		return nil, err
	}

	l := &Loader{
		opts:         opts,
		listeners:    make(map[string][]func(old, new any)),
		envWhitelist: toSet(opts.EnvWhitelist, defaultEnvWhitelist),
		hotReload:    toSetOrNil(opts.HotReloadable),
		remoteRaw:    make(map[string][]byte),
		schema:       schema,
	}

	// 1. 本地 fallback。
	vp := viper.New()
	vp.SetConfigType(opts.LocalConfigType)
	vp.SetConfigFile(opts.LocalPath)
	if err := vp.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read local %s: %w", opts.LocalPath, err)
	}

	// 2. Remote override。
	if opts.Remote != nil {
		fctx, cancel := context.WithTimeout(ctx, opts.FetchTimeout)
		start := time.Now()
		raws, err := opts.Remote.Fetch(fctx)
		elapsedMs := float64(time.Since(start).Milliseconds())
		cancel()
		metricsFetchDurationObserve(opts.ServiceName, opts.Remote.Name(), elapsedMs)
		if err != nil {
			metricsFetchTotalInc(opts.ServiceName, opts.Remote.Name(), "error")
			metricsNacosDisconnectInc()
			metricsLocalFallbackUsedInc(opts.ServiceName)
			l.logf("warn", "remote fetch failed, fall back to local",
				"source", opts.Remote.Name(), "err", err.Error())
		} else {
			metricsFetchTotalInc(opts.ServiceName, opts.Remote.Name(), "ok")
			l.remoteMu.Lock()
			for dataId, raw := range raws {
				l.remoteRaw[dataId] = raw
				if mergeErr := mergeYamlInto(vp, raw); mergeErr != nil {
					l.logf("warn", "remote merge failed",
						"data_id", dataId, "err", mergeErr.Error())
				}
			}
			l.remoteMu.Unlock()
		}
	}

	// 3. env 展开 + 4. ENC 解密 + 落地为新 Viper。
	finalVp, err := l.normalize(vp)
	if err != nil {
		return nil, err
	}

	// 5. Schema 校验（启动阶段校验失败直接拒绝启动，避免带病运行）。
	if err := validateAgainstSchema(l.schema, finalVp.AllSettings()); err != nil {
		metricsValidationFailureInc(opts.ServiceName)
		return nil, err
	}

	l.vp.Store(finalVp)

	// 6. 订阅远端变更。
	if opts.Remote != nil {
		listenCtx := context.Background() // 长生命周期；外部用 Close() 关闭
		cancel, err := opts.Remote.Listen(listenCtx, l.onRemoteChange)
		if err != nil {
			l.logf("warn", "remote listen failed", "source", opts.Remote.Name(), "err", err.Error())
		} else {
			l.remoteCancel = cancel
		}
	}

	return l, nil
}

// normalize 在原始 vp 上做 env 展开 + ENC 解密，返回新的 vp。
// 原 vp 不被修改，方便重组场景复用。
//
// 加密密钥从 vp 中按 EncryptKeyPath 读取。配置中没有 ENC(...) 时无需密钥。
func (l *Loader) normalize(src *viper.Viper) (*viper.Viper, error) {
	settings := src.AllSettings()

	// env 展开
	expandEnvInPlace(settings, l.envWhitelist)

	// ENC 解密：依次尝试每个密钥，能解开任一即可。
	if hasEncrypted(settings) {
		encKeys, err := l.parseEncryptKeysFromConfig(src)
		if err != nil {
			return nil, err
		}
		if len(encKeys) == 0 {
			return nil, fmt.Errorf("config: encrypted value found but %q is empty in config", l.opts.EncryptKeyPath)
		}
		var lastErr error
		decrypted := false
		for _, k := range encKeys {
			cloned := deepClone(settings)
			if err := encrypt.DecryptMap(k, cloned); err == nil {
				settings = cloned
				decrypted = true
				break
			} else {
				lastErr = err
			}
		}
		if !decrypted {
			return nil, fmt.Errorf("config: decrypt failed with all keys: %w", lastErr)
		}
	}

	// 构造新 Viper 承载结果。
	dst := viper.New()
	dst.SetConfigType(l.opts.LocalConfigType)
	for k, v := range settings {
		dst.Set(k, v)
	}
	return dst, nil
}

// parseEncryptKeysFromConfig 从 viper 中按 EncryptKeyPath 读取密钥。
// 支持单个 string（含逗号分隔多密钥）或 []string；缺失返回空切片，nil 错误。
func (l *Loader) parseEncryptKeysFromConfig(vp *viper.Viper) ([][]byte, error) {
	path := l.opts.EncryptKeyPath
	if !vp.IsSet(path) {
		return nil, nil
	}
	raw := vp.Get(path)

	var rawKeys []string
	switch v := raw.(type) {
	case string:
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				rawKeys = append(rawKeys, k)
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					rawKeys = append(rawKeys, s)
				}
			}
		}
	case []string:
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				rawKeys = append(rawKeys, s)
			}
		}
	default:
		return nil, fmt.Errorf("config: %q must be string or []string, got %T", path, raw)
	}

	keys := make([][]byte, 0, len(rawKeys))
	for _, raw := range rawKeys {
		k, err := encrypt.ParseEncryptKey(raw)
		if err != nil {
			return nil, fmt.Errorf("config: parse encrypt key at %q: %w", path, err)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// onRemoteChange 远端推送回调。
func (l *Loader) onRemoteChange(dataId string, raw []byte) {
	l.remoteMu.Lock()
	l.remoteRaw[dataId] = raw
	rawCopies := make(map[string][]byte, len(l.remoteRaw))
	for k, v := range l.remoteRaw {
		rawCopies[k] = v
	}
	l.remoteMu.Unlock()

	// 重新基于本地 + 全部 remote 重组 vp。
	rebuilt := viper.New()
	rebuilt.SetConfigType(l.opts.LocalConfigType)
	rebuilt.SetConfigFile(l.opts.LocalPath)
	if err := rebuilt.ReadInConfig(); err != nil {
		l.logf("error", "reload local fail on remote change", "err", err.Error())
		return
	}
	for id, raw := range rawCopies {
		if err := mergeYamlInto(rebuilt, raw); err != nil {
			l.logf("warn", "merge remote on change", "data_id", id, "err", err.Error())
		}
	}

	final, err := l.normalize(rebuilt)
	if err != nil {
		l.logf("error", "normalize on remote change", "err", err.Error())
		return
	}

	// 热更阶段 Schema 校验失败：保留旧配置、记录指标和错误日志，绝不替换 vp。
	if err := validateAgainstSchema(l.schema, final.AllSettings()); err != nil {
		metricsValidationFailureInc(l.opts.ServiceName)
		l.logf("error", "schema validation failed on hot reload, keep previous config",
			"data_id", dataId, "err", err.Error())
		return
	}

	old := l.vp.Load()
	l.vp.Store(final)

	// 计算 diff，仅对发生变化且 hot-reloadable 的 key 触发 listeners。
	l.fireDiff(old, final)
	l.logf("info", "config reloaded", "data_id", dataId)
	metricsListenTotalInc(l.opts.ServiceName, dataId)
}

// fireDiff 计算两个 viper 间的 dot-key 差异并触发对应监听器。
func (l *Loader) fireDiff(oldVp, newVp *viper.Viper) {
	if oldVp == nil || newVp == nil {
		return
	}
	oldFlat := flatten("", oldVp.AllSettings())
	newFlat := flatten("", newVp.AllSettings())

	// 收集变更的 dot-key 集合。
	changed := map[string][2]any{}
	for k, v := range newFlat {
		if ov, ok := oldFlat[k]; !ok || !equalScalar(ov, v) {
			changed[k] = [2]any{oldFlat[k], v}
		}
	}
	for k, v := range oldFlat {
		if _, ok := newFlat[k]; !ok {
			changed[k] = [2]any{v, nil}
		}
	}
	if len(changed) == 0 {
		return
	}

	// 触发 listeners：listener 注册的 prefix 与变更 key 任一前缀匹配即触发。
	l.mu.RLock()
	listeners := make(map[string][]func(old, new any), len(l.listeners))
	for k, v := range l.listeners {
		listeners[k] = append([]func(old, new any){}, v...)
	}
	l.mu.RUnlock()

	for prefix, fns := range listeners {
		for changedKey, ov := range changed {
			if !keyMatchesPrefix(changedKey, prefix) {
				continue
			}
			if !l.isHotReloadable(changedKey) {
				l.logf("warn", "config change requires restart, skip listener",
					"key", changedKey)
				continue
			}
			for _, fn := range fns {
				safeFire(fn, ov[0], ov[1])
			}
			break // 该 prefix 只触发一次
		}
	}
}

func (l *Loader) isHotReloadable(key string) bool {
	if l.hotReload == nil {
		return true
	}
	for prefix := range l.hotReload {
		if keyMatchesPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// Close 关闭远端订阅。
func (l *Loader) Close() error {
	if l.remoteCancel != nil {
		return l.remoteCancel()
	}
	return nil
}

// ---------------- 强类型读取 ----------------

func (l *Loader) Get(key string) any            { return l.vp.Load().Get(key) }
func (l *Loader) GetString(key string) string   { return l.vp.Load().GetString(key) }
func (l *Loader) GetInt(key string) int         { return l.vp.Load().GetInt(key) }
func (l *Loader) GetInt64(key string) int64     { return l.vp.Load().GetInt64(key) }
func (l *Loader) GetBool(key string) bool       { return l.vp.Load().GetBool(key) }
func (l *Loader) GetFloat64(key string) float64 { return l.vp.Load().GetFloat64(key) }
func (l *Loader) GetStringSlice(key string) []string {
	return l.vp.Load().GetStringSlice(key)
}
func (l *Loader) GetStringMap(key string) map[string]any {
	return l.vp.Load().GetStringMap(key)
}
func (l *Loader) GetDuration(key string) time.Duration {
	return l.vp.Load().GetDuration(key)
}
func (l *Loader) IsSet(key string) bool { return l.vp.Load().IsSet(key) }

// MustHas 缺 key 直接 panic（保持与旧 config 包语义一致）。
func (l *Loader) MustHas(key string) {
	if l.GetString(key) == "" {
		panic(fmt.Sprintf("config: missing required key %q", key))
	}
}

// Bind 把某个 dot-key 子树反序列化到 target（必须是 *struct）。
//
// 走 viper 的 UnmarshalKey，等价于 mapstructure 的逻辑。
func (l *Loader) Bind(prefix string, target any) error {
	if target == nil {
		return errors.New("config: Bind target is nil")
	}
	vp := l.vp.Load()
	if prefix == "" {
		return vp.Unmarshal(target)
	}
	return vp.UnmarshalKey(prefix, target)
}

// OnChange 注册前缀订阅，prefix 子树有热更新时调用 fn。
//
//   - prefix=""           匹配全部
//   - prefix="server"     匹配 server.* 任意深度
//   - prefix="server.tcp" 匹配 server.tcp.* 任意深度
//
// 不在 HotReloadable 白名单内的变更不会触发，仅记录日志。
func (l *Loader) OnChange(prefix string, fn func(old, new any)) {
	l.mu.Lock()
	l.listeners[prefix] = append(l.listeners[prefix], fn)
	l.mu.Unlock()
}

// Set 用于测试或手动覆盖某 key。生产代码请勿使用。
func (l *Loader) Set(key string, value any) {
	l.vp.Load().Set(key, value)
}

func (l *Loader) logf(level, msg string, kv ...any) {
	if l.opts.Logger != nil {
		l.opts.Logger(level, msg, kv...)
		return
	}
	// 兜底打到 stderr。
	var b strings.Builder
	b.WriteString("[compkg/config][")
	b.WriteString(level)
	b.WriteString("] ")
	b.WriteString(msg)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %v=%v", kv[i], kv[i+1])
	}
	fmt.Fprintln(os.Stderr, b.String())
}

// ---------------- 内部工具 ----------------

func mergeYamlInto(vp *viper.Viper, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	tmp := viper.New()
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(raw)); err != nil {
		return err
	}
	return vp.MergeConfigMap(tmp.AllSettings())
}

func toSet(values []string, fallback []string) map[string]struct{} {
	src := values
	if len(src) == 0 {
		src = fallback
	}
	s := make(map[string]struct{}, len(src))
	for _, v := range src {
		if v == "" {
			continue
		}
		s[v] = struct{}{}
	}
	return s
}

func toSetOrNil(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(values))
	for _, v := range values {
		s[v] = struct{}{}
	}
	return s
}

// expandEnvInPlace 原地把字符串中的 ${VAR} 展开为对应环境变量值。仅展开白名单内变量。
func expandEnvInPlace(m map[string]any, whitelist map[string]struct{}) {
	for k, v := range m {
		switch tv := v.(type) {
		case string:
			m[k] = expandStr(tv, whitelist)
		case map[string]any:
			expandEnvInPlace(tv, whitelist)
		case []any:
			for i, e := range tv {
				switch ev := e.(type) {
				case string:
					tv[i] = expandStr(ev, whitelist)
				case map[string]any:
					expandEnvInPlace(ev, whitelist)
				}
			}
		}
	}
}

func expandStr(s string, whitelist map[string]struct{}) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, func(name string) string {
		if _, ok := whitelist[name]; !ok {
			// 不在白名单的占位保留原样，避免误展开机密。
			return "${" + name + "}"
		}
		return os.Getenv(name)
	})
}

func hasEncrypted(m map[string]any) bool {
	for _, v := range m {
		switch tv := v.(type) {
		case string:
			if encrypt.IsEncrypted(tv) {
				return true
			}
		case map[string]any:
			if hasEncrypted(tv) {
				return true
			}
		case []any:
			for _, e := range tv {
				switch ev := e.(type) {
				case string:
					if encrypt.IsEncrypted(ev) {
						return true
					}
				case map[string]any:
					if hasEncrypted(ev) {
						return true
					}
				}
			}
		}
	}
	return false
}

func deepClone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		return deepClone(tv)
	case []any:
		s := make([]any, len(tv))
		for i, e := range tv {
			s[i] = cloneValue(e)
		}
		return s
	default:
		return v
	}
}

// flatten 把嵌套 map 拉平为 dot-key map。
func flatten(prefix string, m map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch tv := v.(type) {
		case map[string]any:
			for sk, sv := range flatten(key, tv) {
				out[sk] = sv
			}
		default:
			out[key] = tv
		}
	}
	return out
}

func equalScalar(a, b any) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func keyMatchesPrefix(key, prefix string) bool {
	if prefix == "" {
		return true
	}
	return key == prefix || strings.HasPrefix(key, prefix+".")
}

func safeFire(fn func(old, new any), old, new any) {
	defer func() {
		_ = recover() // listener 抛 panic 不应影响 SDK
	}()
	fn(old, new)
}

// ---------------- Feature ----------------

// Feature 计算特性开关是否对当前 ctx 生效。
//
// 配置形如 feature.flags.yaml:
//
//	flags:
//	  use_dispatch_queue:
//	    enabled: true
//	    rollout: 50
//
// 灰度算法：crc32(deviceNo|accountId|instanceId) % 100 < rollout。
// rollout >= 100 等价于 enabled=true && 全量；rollout <= 0 时仅 enabled 不足以放行。
func (l *Loader) Feature(name string, ctx FeatureContext) bool {
	var f FeatureFlag
	key := "flags." + name
	if !l.IsSet(key) {
		return false
	}
	if err := l.Bind(key, &f); err != nil {
		l.logf("warn", "feature bind fail", "name", name, "err", err.Error())
		return false
	}
	if !f.Enabled {
		return false
	}
	if f.Rollout >= 100 {
		return true
	}
	if f.Rollout <= 0 {
		return false
	}
	bucket := featureBucket(ctx)
	return bucket < uint32(f.Rollout)
}

func featureBucket(ctx FeatureContext) uint32 {
	var b strings.Builder
	b.WriteString(ctx.DeviceNo)
	b.WriteByte('|')
	fmt.Fprintf(&b, "%d", ctx.AccountId)
	b.WriteByte('|')
	b.WriteString(ctx.InstanceId)
	return crc32.ChecksumIEEE([]byte(b.String())) % 100
}
