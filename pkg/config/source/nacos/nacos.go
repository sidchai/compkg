// Package nacos 提供 RemoteSource 的 Nacos 实现，单独成包以便用户在不需要 Nacos 时不引入 nacos-sdk-go 依赖。
package nacos

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"github.com/sidchai/compkg/pkg/config"
)

// Options Nacos 客户端选项。
type Options struct {
	// Servers Nacos 服务端地址列表，形如 "127.0.0.1:8848"。
	Servers []string

	// NamespaceId Nacos namespace 唯一 ID（注意不是 name）。dev/test/prod 需各自创建。
	NamespaceId string

	// Group 默认 group，按 RFC-06 推荐为业务大类，如 "DEFAULT_GROUP" / "infra" / "service.iot_cloud_platform_server"。
	Group string

	// DataIds 启动时拉取的 dataId 列表，每项形如 "infra.redis.cluster.yaml" / "service.s3_iot_server.yaml"。
	// 全部以 yaml 解析。
	DataIds []string

	// Username/Password Nacos 鉴权（可选，开启鉴权后必填）。
	Username string
	Password string

	// AccessKey/SecretKey 阿里云 Nacos 接入（可选）。
	AccessKey string
	SecretKey string

	// LogDir/CacheDir/LogLevel SDK 内部使用，可选。
	LogDir   string
	CacheDir string
	LogLevel string

	// TimeoutMs 请求超时，默认 5000。
	TimeoutMs uint64

	// NotLoadCacheAtStart 启动时是否跳过本地缓存（默认 false，即用本地缓存兜底）。
	NotLoadCacheAtStart bool
}

// Source 实现 config.RemoteSource。
type Source struct {
	opts Options
	cli  config_client.IConfigClient

	mu        sync.Mutex
	cancelers []func() error
}

// New 构造 Nacos source 但不立即建立连接；连接在 Fetch/Listen 内部按需创建。
func New(opts Options) (*Source, error) {
	if len(opts.Servers) == 0 {
		return nil, errors.New("nacos: servers required")
	}
	if len(opts.DataIds) == 0 {
		return nil, errors.New("nacos: dataIds required")
	}
	if opts.Group == "" {
		opts.Group = "DEFAULT_GROUP"
	}
	if opts.TimeoutMs == 0 {
		opts.TimeoutMs = 5000
	}
	if opts.LogLevel == "" {
		opts.LogLevel = "warn"
	}
	return &Source{opts: opts}, nil
}

// Name 返回源名称（多源时用于区分）。
func (s *Source) Name() string { return "nacos" }

// ensureClient 懒初始化。
func (s *Source) ensureClient() error {
	if s.cli != nil {
		return nil
	}
	serverConfigs := make([]constant.ServerConfig, 0, len(s.opts.Servers))
	for _, addr := range s.opts.Servers {
		host, port, err := splitHostPort(addr)
		if err != nil {
			return fmt.Errorf("nacos: invalid server %q: %w", addr, err)
		}
		serverConfigs = append(serverConfigs, *constant.NewServerConfig(host, port))
	}
	clientCfg := *constant.NewClientConfig(
		constant.WithNamespaceId(s.opts.NamespaceId),
		constant.WithTimeoutMs(s.opts.TimeoutMs),
		constant.WithUsername(s.opts.Username),
		constant.WithPassword(s.opts.Password),
		constant.WithAccessKey(s.opts.AccessKey),
		constant.WithSecretKey(s.opts.SecretKey),
		constant.WithLogDir(s.opts.LogDir),
		constant.WithCacheDir(s.opts.CacheDir),
		constant.WithLogLevel(s.opts.LogLevel),
		constant.WithNotLoadCacheAtStart(s.opts.NotLoadCacheAtStart),
	)
	cli, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &clientCfg,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		return fmt.Errorf("nacos: new client: %w", err)
	}
	s.cli = cli
	return nil
}

// Fetch 一次性拉取所有 dataId。失败返回部分成功的 map + error。
func (s *Source) Fetch(ctx context.Context) (map[string][]byte, error) {
	if err := s.ensureClient(); err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(s.opts.DataIds))
	var firstErr error
	for _, dataId := range s.opts.DataIds {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		raw, err := s.cli.GetConfig(vo.ConfigParam{
			DataId: dataId,
			Group:  s.opts.Group,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("nacos: get %s/%s: %w", s.opts.Group, dataId, err)
			}
			continue
		}
		if raw == "" {
			// Nacos 上不存在该 dataId 时返回空字符串；视为可选配置。
			continue
		}
		out[dataId] = []byte(raw)
	}
	return out, firstErr
}

// Listen 订阅每个 dataId 的变更。
func (s *Source) Listen(_ context.Context, onChange func(dataId string, raw []byte)) (func() error, error) {
	if err := s.ensureClient(); err != nil {
		return nil, err
	}
	for _, dataId := range s.opts.DataIds {
		dataId := dataId
		err := s.cli.ListenConfig(vo.ConfigParam{
			DataId: dataId,
			Group:  s.opts.Group,
			OnChange: func(namespace, group, dId, data string) {
				onChange(dId, []byte(data))
			},
		})
		if err != nil {
			// 已订阅的不回滚，直接返回错误让 Loader 决定是否退出。
			return s.cancelAll, fmt.Errorf("nacos: listen %s/%s: %w", s.opts.Group, dataId, err)
		}
		dID := dataId
		s.mu.Lock()
		s.cancelers = append(s.cancelers, func() error {
			return s.cli.CancelListenConfig(vo.ConfigParam{
				DataId: dID,
				Group:  s.opts.Group,
			})
		})
		s.mu.Unlock()
	}
	return s.cancelAll, nil
}

func (s *Source) cancelAll() error {
	s.mu.Lock()
	cancels := s.cancelers
	s.cancelers = nil
	s.mu.Unlock()
	var firstErr error
	for _, c := range cancels {
		if err := c(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// 类型断言：编译期校验实现接口。
var _ config.RemoteSource = (*Source)(nil)

func splitHostPort(addr string) (string, uint64, error) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host := addr[:i]
			portStr := addr[i+1:]
			var port uint64
			for _, c := range portStr {
				if c < '0' || c > '9' {
					return "", 0, fmt.Errorf("invalid port %q", portStr)
				}
				port = port*10 + uint64(c-'0')
			}
			if host == "" || port == 0 {
				return "", 0, fmt.Errorf("invalid addr %q", addr)
			}
			return host, port, nil
		}
	}
	return "", 0, fmt.Errorf("missing port in %q", addr)
}
