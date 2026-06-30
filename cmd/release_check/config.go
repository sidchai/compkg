package main

import (
	"fmt"
	"os"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// CheckConfig 发版检测工具的总配置（独立于业务 yaml，避免耦合）。
type CheckConfig struct {
	Redis    RedisConf       `yaml:"redis"`    // Redis 连接（须与各服务心跳上报的同一实例/库）
	Prefix   string          `yaml:"prefix"`   // health key 前缀，默认 compkg，须与 Reporter 一致
	Report   ReportConf      `yaml:"report"`   // 报告输出配置
	Defaults DefaultsConf    `yaml:"defaults"` // 服务级默认值
	Services []ServiceTarget `yaml:"services"` // 被检测服务清单
}

// RedisConf Redis 连接参数。
type RedisConf struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	Db       int    `yaml:"db"`
}

// ReportConf 报告输出。
type ReportConf struct {
	Output string `yaml:"output"` // Markdown 报告输出路径，空则只打印到 stdout
}

// DefaultsConf 服务级默认值，未在 service 内单独指定时回退到此。
type DefaultsConf struct {
	AliveWindow string `yaml:"aliveWindow"` // 实例存活窗口，如 "30s"，默认 30s

	aliveWindow time.Duration `yaml:"-"` // 解析后的存活窗口（内部使用）
}

// AliveWindow 返回解析后的存活窗口。
func (d DefaultsConf) AliveWindowDuration() time.Duration { return d.aliveWindow }

// ServiceTarget 单个被检测服务。
type ServiceTarget struct {
	Name         string `yaml:"name"`         // 心跳 service 名（如 iot_cloud_platform_server）
	Display      string `yaml:"display"`      // 展示名（如 管理后台服务）
	HighRisk     bool   `yaml:"highRisk"`     // 高危服务：检测失败导致整体退出码非 0
	ExpectCommit string `yaml:"expectCommit"` // 预期 commit；auto/空=用命令行 -commit；具体值=覆盖
	MinInstances int    `yaml:"minInstances"` // 期望最少存活实例数，默认 1
}

// LoadConfig 读取并校验检测配置。
func LoadConfig(path string) (*CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	cfg := &CheckConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if cfg.Redis.Addr == "" {
		return nil, fmt.Errorf("redis.addr 不能为空")
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "compkg"
	}
	if cfg.Defaults.AliveWindow == "" {
		cfg.Defaults.aliveWindow = 30 * time.Second
	} else {
		d, err := time.ParseDuration(cfg.Defaults.AliveWindow)
		if err != nil {
			return nil, fmt.Errorf("defaults.aliveWindow 格式错误 %q: %w", cfg.Defaults.AliveWindow, err)
		}
		cfg.Defaults.aliveWindow = d
	}
	if len(cfg.Services) == 0 {
		return nil, fmt.Errorf("services 不能为空")
	}
	for i := range cfg.Services {
		if cfg.Services[i].Name == "" {
			return nil, fmt.Errorf("services[%d].name 不能为空", i)
		}
		if cfg.Services[i].MinInstances <= 0 {
			cfg.Services[i].MinInstances = 1
		}
		if cfg.Services[i].Display == "" {
			cfg.Services[i].Display = cfg.Services[i].Name
		}
	}
	return cfg, nil
}
