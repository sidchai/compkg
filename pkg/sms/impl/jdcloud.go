package impl

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/sms"
)

// 京东云通信云文本短信平台 HTTP 协议 v6.0.7
// 文档：下行 POST form-urlencoded → /HttpSmsMt；鉴权 MD5(pwd+mttime)，pwd 为 32 位 MD5 小写密码。
const (
	jdcloudDefaultTimeout = 10 * time.Second
	jdcloudSuccessCode    = "00"
)

// 内容模板占位符：{varName}
var jdcloudPlaceholder = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// JdcloudSMS 京东云文本短信实现（内容直发，非云端模板 ID 模式）。
// 与阿里云差异：
//   - templateCode 在此实现中表示「本地内容模板」，支持 {key} 占位符；
//   - 签名通过 SignName 自动加【】前缀（内容已含【】则不再重复）；
//   - 多签名时必须配置 SubId（扩展码），一个扩展码对应一个报备签名。
type JdcloudSMS struct {
	account    string // name：平台分配账号
	password   string // 32 位 MD5 小写密码（平台给定或对明文二次 MD5 后的值）
	signName   string
	endpoint   string // 完整下行 URL，如 https://x.x.x.x:port/HttpSmsMt
	subId      string // 扩展码 subid
	httpClient *http.Client
}

func init() {
	sms.RegisterProvider(sms.ProviderJdcloud, func() sms.Provider {
		return &JdcloudSMS{}
	})
}

// NewClient 初始化京东云短信客户端。
// AccessKeyId → name；AccessKeySecret → 32 位 MD5 密码；Endpoint → 完整 HttpSmsMt 地址；SubId → 扩展码。
func (j *JdcloudSMS) NewClient(opts ...sms.Option) {
	cfg := sms.DefaultOptions
	for _, opt := range opts {
		opt.Apply(&cfg)
	}

	j.account = cfg.AccessKeyId
	j.password = normalizeJdcloudPassword(cfg.AccessKeySecret)
	j.signName = cfg.SignName
	j.endpoint = strings.TrimSpace(cfg.Endpoint)
	j.subId = cfg.SubId

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = jdcloudDefaultTimeout
	}
	j.httpClient = &http.Client{Timeout: timeout}
}

// Send 按协议提交下行短信。
// templateCode：本地内容模板，如 "您的验证码是{code}，5分钟内有效"；
// templateParams：占位符替换；若含 "content" 键则直接使用该值作为正文（忽略模板）。
func (j *JdcloudSMS) Send(ctx context.Context, phone, templateCode string, templateParams map[string]string) (*sms.SendResult, error) {
	if j.httpClient == nil {
		return nil, fmt.Errorf("jdcloud sms: client not initialized, call NewClient first")
	}
	if j.account == "" {
		return nil, fmt.Errorf("jdcloud sms: account(name) is empty")
	}
	if j.password == "" {
		return nil, fmt.Errorf("jdcloud sms: password is empty")
	}
	if j.endpoint == "" {
		return nil, fmt.Errorf("jdcloud sms: endpoint is empty, need full HttpSmsMt URL from JD")
	}
	if phone == "" {
		return nil, fmt.Errorf("jdcloud sms: phone is empty")
	}

	content, err := j.buildContent(templateCode, templateParams)
	if err != nil {
		return nil, err
	}
	// 协议要求：半角 % 易造成乱码，替换为全角％；同理处理 & +
	content = sanitizeJdcloudContent(content)
	if content == "" {
		return nil, fmt.Errorf("jdcloud sms: content is empty")
	}
	if len([]rune(content)) > 1000 {
		return nil, fmt.Errorf("jdcloud sms: content exceeds 1000 characters")
	}

	// mttime 必须贴近当前时间；pwd = MD5(pwd32 + mttime)
	mttime := time.Now().Format("20060102150405")
	pwd := md5HexLower(j.password + mttime)

	form := url.Values{}
	form.Set("name", j.account)
	form.Set("pwd", pwd)
	form.Set("phone", phone)
	form.Set("content", content)
	form.Set("mttime", mttime)
	if j.subId != "" {
		form.Set("subid", j.subId)
	}
	// 可选扩展字段：业务侧可通过 templateParams["_extend"] 传入，最长 32 位
	if extend := strings.TrimSpace(templateParams["_extend"]); extend != "" {
		if len(extend) > 32 {
			extend = extend[:32]
		}
		form.Set("extend", extend)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("jdcloud sms: create request: %w", err)
	}
	// 协议强制 form-urlencoded，禁止 JSON payload
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := j.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jdcloud sms: http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("jdcloud sms: read response: %w", err)
	}

	log.Println("jdcloud sms body:", string(body))

	var raw jdcloudSendResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("jdcloud sms: parse response: %w, body=%s", err, string(body))
	}

	result := &sms.SendResult{
		Code:      raw.ReqCode,
		Message:   raw.ReqMsg,
		BizId:     raw.ReqId,
		RequestId: raw.ReqId, // 协议无独立 RequestId，用批次 ID 填充便于排障
	}
	if raw.ReqCode != jdcloudSuccessCode {
		return result, fmt.Errorf("jdcloud sms: send failed: %s - %s", raw.ReqCode, raw.ReqMsg)
	}
	return result, nil
}

// buildContent 将本地模板 + 参数渲染为最终短信正文，并按需附加签名。
func (j *JdcloudSMS) buildContent(templateCode string, params map[string]string) (string, error) {
	if params == nil {
		params = map[string]string{}
	}
	// 显式 content 优先，兼容「直接传正文」调用方式
	if c := strings.TrimSpace(params["content"]); c != "" {
		return j.withSign(c), nil
	}
	if strings.TrimSpace(templateCode) == "" {
		return "", fmt.Errorf("jdcloud sms: templateCode(content template) is empty")
	}

	content := jdcloudPlaceholder.ReplaceAllStringFunc(templateCode, func(m string) string {
		sub := jdcloudPlaceholder.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if v, ok := params[sub[1]]; ok {
			return v
		}
		return m
	})
	return j.withSign(content), nil
}

// withSign 在正文前加【签名】；正文已含【】时不重复追加（避免双签名）。
func (j *JdcloudSMS) withSign(content string) string {
	content = strings.TrimSpace(content)
	if j.signName == "" || strings.Contains(content, "【") {
		return content
	}
	return "【" + j.signName + "】" + content
}

// jdcloudSendResponse 下行接口 JSON 响应
type jdcloudSendResponse struct {
	ReqCode string `json:"ReqCode"`
	ReqMsg  string `json:"ReqMsg"`
	ReqId   string `json:"ReqId"`
}

// normalizeJdcloudPassword 规范密码为 32 位 MD5 小写。
// 若配置已是 32 位十六进制则直接使用；否则对明文再做一次 MD5（兼容运营误配明文密码）。
func normalizeJdcloudPassword(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if isMD5Hex32(secret) {
		return strings.ToLower(secret)
	}
	return md5HexLower(secret)
}

func isMD5Hex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func md5HexLower(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// sanitizeJdcloudContent 按协议建议替换易导致乱码的半角字符。
func sanitizeJdcloudContent(content string) string {
	content = strings.ReplaceAll(content, "%", "％")
	content = strings.ReplaceAll(content, "&", "＆")
	content = strings.ReplaceAll(content, "+", "＋")
	return content
}
