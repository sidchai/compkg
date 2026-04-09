package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/alert"
)

// KVReader 键值读取接口 — 零外部依赖
// 调用方用自己的 Redis/KV 客户端实现此接口即可
type KVReader interface {
	Get(ctx context.Context, key string) (string, error)
}

const (
	defaultRedisKey = "alert:config"
	defaultCacheTTL = 30 * time.Second
)

// KVOption KV 配置提供者选项
type KVOption func(*KVConfigProvider)

// WithKey 设置 Redis key
func WithKey(key string) KVOption {
	return func(p *KVConfigProvider) {
		p.key = key
	}
}

// WithCacheTTL 设置本地缓存过期时间
func WithCacheTTL(ttl time.Duration) KVOption {
	return func(p *KVConfigProvider) {
		p.cacheTTL = ttl
	}
}

// KVConfigProvider 基于 KV 存储的告警配置提供者
// 支持 Redis、Etcd、Consul 等任何实现了 KVReader 接口的 KV 存储
type KVConfigProvider struct {
	reader   KVReader
	key      string
	cache    *alert.NotifierConfig
	cacheAt  time.Time
	cacheTTL time.Duration
	mu       sync.RWMutex
	fallback *alert.NotifierConfig
}

// NewKVProvider 创建 KV 配置提供者
// reader: KV 读取器（调用方自行适配 Redis 客户端）
// fallback: Redis 不可用时的降级配置
func NewKVProvider(reader KVReader, fallback *alert.NotifierConfig, opts ...KVOption) *KVConfigProvider {
	p := &KVConfigProvider{
		reader:   reader,
		key:      defaultRedisKey,
		cacheTTL: defaultCacheTTL,
		fallback: fallback,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// GetNotifierConfig 获取最新通知配置
// 优先使用本地缓存，缓存过期后从 KV 存储读取
// KV 不可用时使用降级配置
func (p *KVConfigProvider) GetNotifierConfig() (*alert.NotifierConfig, error) {
	// 本地缓存未过期，直接返回
	p.mu.RLock()
	if p.cache != nil && time.Since(p.cacheAt) < p.cacheTTL {
		cfg := *p.cache
		p.mu.RUnlock()
		return &cfg, nil
	}
	p.mu.RUnlock()

	// 从 KV 存储读取
	val, err := p.reader.Get(context.Background(), p.key)
	if err != nil {
		// KV 不可用，使用降级配置
		if p.fallback != nil {
			return p.fallback, nil
		}
		return nil, err
	}

	cfg := &alert.NotifierConfig{}
	if err := json.Unmarshal([]byte(val), cfg); err != nil {
		// 解析失败，使用降级配置
		if p.fallback != nil {
			return p.fallback, nil
		}
		return nil, err
	}

	// 更新本地缓存
	p.mu.Lock()
	p.cache = cfg
	p.cacheAt = time.Now()
	p.mu.Unlock()

	return cfg, nil
}

// InvalidateCache 手动使缓存失效（用于测试或强制刷新）
func (p *KVConfigProvider) InvalidateCache() {
	p.mu.Lock()
	p.cache = nil
	p.mu.Unlock()
}

// ==================== 规则动态加载 ====================

// RuleJSON 运维友好的规则 JSON 格式
// cooldown_seconds 用整数秒表示冷却时间，比 time.Duration 的纳秒更直观
type RuleJSON struct {
	MetricName      string  `json:"metric"`           // 对应的埋点名
	RuleType        string  `json:"type"`             // threshold / rate / surge / consecutive / immediate
	Level           int     `json:"level"`            // 0=P0, 1=P1, 2=P2
	Title           string  `json:"title"`            // 告警标题
	Threshold       float64 `json:"threshold"`        // 阈值
	ConsecutiveN    int     `json:"consecutive_n"`    // 连续次数
	CooldownSeconds int     `json:"cooldown_seconds"` // 冷却秒数
}

// GetRules 从 KV 存储读取动态规则列表
// rulesKey: 规则列表的 Redis key（如 "alert:rules"）
// 返回解析后的 Rule 切片；KV 不可用或 key 不存在时返回 nil, err
func (p *KVConfigProvider) GetRules(rulesKey string) ([]alert.Rule, error) {
	val, err := p.reader.Get(context.Background(), rulesKey)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return nil, nil
	}

	var jsonRules []RuleJSON
	if err := json.Unmarshal([]byte(val), &jsonRules); err != nil {
		return nil, fmt.Errorf("parse rules json: %w", err)
	}

	rules := make([]alert.Rule, 0, len(jsonRules))
	for _, jr := range jsonRules {
		if jr.MetricName == "" {
			continue
		}
		cooldown := time.Duration(jr.CooldownSeconds) * time.Second
		if cooldown <= 0 {
			cooldown = 5 * time.Minute // 默认5分钟
		}
		rules = append(rules, alert.Rule{
			MetricName:     jr.MetricName,
			RuleType:       alert.ParseRuleType(jr.RuleType),
			Level:          alert.Level(jr.Level),
			Title:          jr.Title,
			Threshold:      jr.Threshold,
			ConsecutiveN:   jr.ConsecutiveN,
			CooldownPeriod: cooldown,
		})
	}
	return rules, nil
}
