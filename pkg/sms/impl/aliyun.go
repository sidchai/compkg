package impl

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/sms"
)

const (
	aliyunDefaultEndpoint = "dysmsapi.aliyuncs.com"
	aliyunDefaultRegion   = "cn-hangzhou"
	aliyunAPIVersion      = "2017-05-25"
	aliyunDefaultTimeout  = 10 * time.Second
)

// AliyunSMS 阿里云短信实现（OpenAPI SendSms，HMAC-SHA1 签名）。
type AliyunSMS struct {
	accessKeyId     string
	accessKeySecret string
	signName        string
	endpoint        string
	regionId        string
	httpClient      *http.Client
}

func init() {
	sms.RegisterProvider(sms.ProviderAliyun, func() sms.Provider {
		return &AliyunSMS{}
	})
}

// NewClient 初始化阿里云短信客户端。
func (a *AliyunSMS) NewClient(opts ...sms.Option) {
	cfg := sms.DefaultOptions
	for _, opt := range opts {
		opt.Apply(&cfg)
	}

	a.accessKeyId = cfg.AccessKeyId
	a.accessKeySecret = cfg.AccessKeySecret
	a.signName = cfg.SignName

	a.endpoint = cfg.Endpoint
	if a.endpoint == "" {
		a.endpoint = aliyunDefaultEndpoint
	}
	a.regionId = cfg.RegionId
	if a.regionId == "" {
		a.regionId = aliyunDefaultRegion
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = aliyunDefaultTimeout
	}
	a.httpClient = &http.Client{Timeout: timeout}
}

// Send 调用阿里云 SendSms 接口发送短信。
// templateCode 与 templateParams 必须与控制台审核通过的模板一致，否则厂商会返回业务错误。
func (a *AliyunSMS) Send(ctx context.Context, phone, templateCode string, templateParams map[string]string) (*sms.SendResult, error) {
	if a.httpClient == nil {
		return nil, fmt.Errorf("aliyun sms: client not initialized, call NewClient first")
	}
	if a.accessKeyId == "" || a.accessKeySecret == "" {
		return nil, fmt.Errorf("aliyun sms: access key is empty")
	}
	if phone == "" {
		return nil, fmt.Errorf("aliyun sms: phone is empty")
	}
	if templateCode == "" {
		return nil, fmt.Errorf("aliyun sms: templateCode is empty")
	}
	if a.signName == "" {
		return nil, fmt.Errorf("aliyun sms: signName is empty")
	}

	paramJSON, err := json.Marshal(templateParams)
	if err != nil {
		return nil, fmt.Errorf("aliyun sms: marshal template params: %w", err)
	}

	params := map[string]string{
		"AccessKeyId":      a.accessKeyId,
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     phone,
		"RegionId":         a.regionId,
		"SignName":         a.signName,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   fmt.Sprintf("%d%d", time.Now().UnixNano(), rand.Intn(10000)),
		"SignatureVersion": "1.0",
		"TemplateCode":     templateCode,
		"TemplateParam":    string(paramJSON),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          aliyunAPIVersion,
	}
	params["Signature"] = aliyunSign(a.accessKeySecret, params)

	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	reqURL := fmt.Sprintf("https://%s/?%s", a.endpoint, values.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("aliyun sms: create request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aliyun sms: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("aliyun sms: read response: %w", err)
	}

	var raw aliyunSendResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("aliyun sms: parse response: %w, body=%s", err, string(body))
	}

	result := &sms.SendResult{
		Code:      raw.Code,
		Message:   raw.Message,
		BizId:     raw.BizId,
		RequestId: raw.RequestId,
	}
	if raw.Code != "OK" {
		return result, fmt.Errorf("aliyun sms: send failed: %s - %s", raw.Code, raw.Message)
	}
	return result, nil
}

// aliyunSendResponse 阿里云 SendSms 原始响应字段
type aliyunSendResponse struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	BizId     string `json:"BizId"`
	RequestId string `json:"RequestId"`
}

// aliyunSign 阿里云 POP 签名（HMAC-SHA1 + 特殊 URL 编码）
func aliyunSign(accessKeySecret string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", aliyunURLEncode(k), aliyunURLEncode(params[k])))
	}
	sortedQuery := strings.Join(pairs, "&")
	stringToSign := "GET&" + aliyunURLEncode("/") + "&" + aliyunURLEncode(sortedQuery)

	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// aliyunURLEncode 阿里云要求的特殊 URL 编码规则
func aliyunURLEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}
