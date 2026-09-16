package sms

import "time"

// Options 短信客户端配置（厂商公共字段；特有字段由各实现自行扩展或忽略）。
type Options struct {
	AccessKeyId     string        // AccessKey / 账号 name（京东云短信账号）
	AccessKeySecret string        // AccessKeySecret / 密码（京东云为 32 位 MD5 小写密码）
	SignName        string        // 短信签名（不含【】；京东云会自动包【】拼进内容）
	Endpoint        string        // 厂商 API 端点；京东云为完整下行地址，如 https://host:port/HttpSmsMt
	RegionId        string        // 地域，空则使用实现默认值
	AppId           string        // 部分厂商需要的应用/SDK AppId（如腾讯云）
	SubId           string        // 扩展码（京东云 subid；多签名时必填，与签名一一对应）
	Timeout         time.Duration // HTTP 超时，0 则使用实现默认值
}

// DefaultOptions 默认配置零值
var DefaultOptions = Options{}

// Option 配置选项接口
type Option interface {
	Apply(*Options)
}

type funcOption struct {
	f func(*Options)
}

func (fo *funcOption) Apply(o *Options) {
	fo.f(o)
}

func newFuncOption(f func(*Options)) *funcOption {
	return &funcOption{f: f}
}

// WithAccessKeyId 设置 AccessKeyId
func WithAccessKeyId(accessKeyId string) Option {
	return newFuncOption(func(o *Options) {
		o.AccessKeyId = accessKeyId
	})
}

// WithAccessKeySecret 设置 AccessKeySecret
func WithAccessKeySecret(accessKeySecret string) Option {
	return newFuncOption(func(o *Options) {
		o.AccessKeySecret = accessKeySecret
	})
}

// WithSignName 设置短信签名
func WithSignName(signName string) Option {
	return newFuncOption(func(o *Options) {
		o.SignName = signName
	})
}

// WithEndpoint 设置 API 端点
func WithEndpoint(endpoint string) Option {
	return newFuncOption(func(o *Options) {
		o.Endpoint = endpoint
	})
}

// WithRegionId 设置地域
func WithRegionId(regionId string) Option {
	return newFuncOption(func(o *Options) {
		o.RegionId = regionId
	})
}

// WithAppId 设置应用 ID（腾讯云等厂商使用）
func WithAppId(appId string) Option {
	return newFuncOption(func(o *Options) {
		o.AppId = appId
	})
}

// WithSubId 设置扩展码（京东云 subid；多签名场景必填）
func WithSubId(subId string) Option {
	return newFuncOption(func(o *Options) {
		o.SubId = subId
	})
}

// WithTimeout 设置 HTTP 请求超时
func WithTimeout(timeout time.Duration) Option {
	return newFuncOption(func(o *Options) {
		o.Timeout = timeout
	})
}
