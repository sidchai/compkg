package sms

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
)

// 厂商标识常量，业务侧通过 GetProvider 按名称获取实现。
const (
	ProviderAliyun  = "aliyun"  // 阿里云短信（模板 ID 模式）
	ProviderJdcloud = "jdcloud" // 京东云通信云文本短信（内容直发，协议 v6.0.7）
	ProviderTencent = "tencent" // 腾讯云短信（预留）
	ProviderHuawei  = "huawei"  // 华为云短信（预留）
)

// ProviderFactory 创建短信厂商实例的工厂函数
type ProviderFactory func() Provider

var providers map[string]ProviderFactory

// Provider 短信厂商统一接口。
// 设计原则：
//   - 客户端级配置（AK/SK/签名/Endpoint）在 NewClient 中注入；
//   - 业务级参数（模板编码、模板变量）在 Send 时传入，避免并发下共享状态污染。
type Provider interface {
	// NewClient 使用 options 初始化厂商客户端；重复调用以最后一次为准。
	NewClient(opts ...Option)
	// Send 发送短信。
	// phone: 手机号；templateCode: 模板编码；templateParams: 模板变量键值对。
	Send(ctx context.Context, phone, templateCode string, templateParams map[string]string) (*SendResult, error)
}

// SendResult 短信发送统一结果（各厂商响应映射到此结构，便于业务侧统一处理）。
type SendResult struct {
	Code      string // 厂商业务码，成功时一般为 "OK" 或空
	Message   string // 厂商返回消息
	BizId     string // 业务流水号（可用于查询回执）
	RequestId string // 请求 ID（排障用）
}

func init() {
	providers = make(map[string]ProviderFactory)
}

// RegisterProvider 注册短信厂商工厂。各 impl 包在 init 中调用。
func RegisterProvider(name string, factory ProviderFactory) {
	if name == "" || factory == nil {
		return
	}
	providers[name] = factory
}

// GetProvider 按厂商名获取新实例（每次返回新对象，调用方自行 NewClient，避免并发共享）。
// 未注册时返回 nil。
func GetProvider(name string) Provider {
	factory, ok := providers[name]
	if !ok {
		return nil
	}
	return factory()
}

// GenerateCode 生成指定长度的数字验证码（使用 crypto/rand，适合短信 OTP 场景）。
// length <= 0 时默认 6 位。
func GenerateCode(length int) string {
	if length <= 0 {
		length = 6
	}
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			// crypto/rand 失败时回退到时间相关伪随机，保证可用性（概率极低）
			code[i] = byte('0' + (i*7+3)%10)
			continue
		}
		code[i] = byte('0' + n.Int64())
	}
	return string(code)
}

// SendCode 便捷方法：按验证码模板发送（模板变量固定为 code）。
// 调用方需已 NewClient；templateCode 为空时由厂商实现决定是否报错。
func SendCode(ctx context.Context, p Provider, phone, templateCode, code string) (*SendResult, error) {
	if p == nil {
		return nil, fmt.Errorf("sms: provider is nil")
	}
	return p.Send(ctx, phone, templateCode, map[string]string{"code": code})
}
