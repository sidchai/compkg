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
	Notify   NotifyConf      `yaml:"notify"`   // 通知配置（钉钉/邮件）
	Defaults DefaultsConf    `yaml:"defaults"` // 服务级默认值
	Services []ServiceTarget `yaml:"services"` // 被检测服务清单
}

// RedisConf Redis 连接参数。
type RedisConf struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	Db       int    `yaml:"db"`
}

// ReportConf 报告输出：按天生成文件，自动清理过期报告。
type ReportConf struct {
	OutputDir string `yaml:"outputDir"` // 报告输出目录，空则只打印到 stdout
	KeepDays  int    `yaml:"keepDays"`  // 报告保留天数，<=0 默认 1（只留当天）
}

// NotifyConf 通知配置。
type NotifyConf struct {
	DingTalk DingTalkConf `yaml:"dingtalk"` // 钉钉机器人
	Email    EmailConf    `yaml:"email"`    // SMTP 邮件
}

// DingTalkConf 钉钉机器人通知配置。
type DingTalkConf struct {
	Enabled       bool   `yaml:"enabled"`       // 是否启用
	Webhook       string `yaml:"webhook"`       // 机器人 webhook URL
	Secret        string `yaml:"secret"`        // 加签密钥（可选）
	OnlyOnFailure bool   `yaml:"onlyOnFailure"` // 仅检测失败时通知
}

// EmailConf SMTP 邮件通知配置。
type EmailConf struct {
	Enabled       bool     `yaml:"enabled"`       // 是否启用
	SmtpHost      string   `yaml:"smtpHost"`      // SMTP 服务器地址
	SmtpPort      int      `yaml:"smtpPort"`      // SMTP 端口（465=SSL, 587=STARTTLS）
	Username      string   `yaml:"username"`      // 登录用户名
	Password      string   `yaml:"password"`      // 登录密码/授权码
	From          string   `yaml:"from"`          // 发件人地址
	To            []string `yaml:"to"`            // 收件人列表
	OnlyOnFailure bool     `yaml:"onlyOnFailure"` // 仅检测失败时通知
}

// DefaultsConf 服务级默认值，未在 service 内单独指定时回退到此。
type DefaultsConf struct {
	AliveWindow string `yaml:"aliveWindow"` // 实例存活窗口，如 "30s"，默认 30s

	aliveWindow time.Duration `yaml:"-"` // 解析后的存活窗口（内部使用）
}

// AliveWindowDuration 返回解析后的存活窗口。
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
	if cfg.Report.KeepDays <= 0 {
		cfg.Report.KeepDays = 1 // 默认只留当天
	}
	// 通知配置校验：启用了才校验必填项，避免没用到的渠道阻断启动。
	if cfg.Notify.DingTalk.Enabled && cfg.Notify.DingTalk.Webhook == "" {
		return nil, fmt.Errorf("notify.dingtalk.enabled=true 但 webhook 为空")
	}
	if cfg.Notify.Email.Enabled {
		e := cfg.Notify.Email
		if e.SmtpHost == "" || e.SmtpPort == 0 || e.From == "" || len(e.To) == 0 {
			return nil, fmt.Errorf("notify.email.enabled=true 但 smtpHost/smtpPort/from/to 配置不完整")
		}
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
