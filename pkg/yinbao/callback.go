package yinbao

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/httptool"
)

// Config 是银保回调加密的运行参数。零值表示关闭，调用方保持原明文。
// 密钥和主机名单由各服务自己的配置装入，本包不读 yaml。
type Config struct {
	Enable        bool
	SystemID      string
	SM4Key        string
	SM3Salt       string
	CallbackHosts []string
}

const (
	headerTimestamp = "timestamp"
	headerNonce     = "nonce"
	headerSystemID  = "systemId"
	headerSign      = "sign"
)

// Applicable 判断这次回调要不要走银保加密。开关关闭或主机不匹配时返回 false。
func (c Config) Applicable(callbackURL string) bool {
	if !c.Enable {
		return false
	}
	return hostMatch(callbackURL, c.CallbackHosts)
}

func hostMatch(callbackURL string, hosts []string) bool {
	if len(hosts) == 0 {
		return false
	}
	parsed, err := url.Parse(callbackURL)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	full := strings.ToLower(parsed.Host)
	for _, item := range hosts {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if item == host || item == full {
			return true
		}
	}
	return false
}

func (c Config) ready() error {
	if strings.TrimSpace(c.SystemID) == "" || c.SM3Salt == "" {
		return errors.New("银保回调缺少 systemId 或 sm3Salt")
	}
	if len(c.SM4Key) != sm4KeyLen {
		return errors.New("银保回调 sm4Key 长度必须是 16 字节")
	}
	return nil
}

// Seal 用明文计算签名并返回 SM4 十六进制密文。签名材料是明文，不是密文。
// headers 会写入 timestamp、nonce、systemId、sign。
func (c Config) Seal(headers map[string]string, plain string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	cipherHex, err := encryptHex([]byte(c.SM4Key), plain)
	if err != nil {
		return "", err
	}
	nonce, err := newNonce()
	if err != nil {
		return "", err
	}
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	headers[headerTimestamp] = timestamp
	headers[headerNonce] = nonce
	headers[headerSystemID] = strings.TrimSpace(c.SystemID)
	headers[headerSign] = signSM3(timestamp, nonce, plain, c.SM3Salt)
	return cipherHex, nil
}

// Post 发送客户回调。
// rawJSON 非空时正文已是 JSON 字符串；否则对 payload 做 json.Marshal，与 httptool.WithData 一致。
// 未命中银保主机时请求字节与直接调用 httptool 相同。命中但密钥不可用时直接失败，不把明文发给对方。
func (c Config) Post(callbackURL string, headers map[string]string, payload interface{}, rawJSON string, timeout time.Duration) (*http.Response, error) {
	if !c.Applicable(callbackURL) {
		if rawJSON != "" {
			return httptool.HTTPRequest(httptool.HttpMethodPost, callbackURL,
				httptool.WithJsonData(rawJSON),
				httptool.WithHeaders(headers),
				httptool.WithTimeout(timeout))
		}
		return httptool.HTTPRequest(httptool.HttpMethodPost, callbackURL,
			httptool.WithData(payload),
			httptool.WithHeaders(headers),
			httptool.WithTimeout(timeout))
	}
	plain := rawJSON
	if plain == "" {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("银保回调序列化: %w", err)
		}
		plain = string(buf)
	}
	wire, err := c.Seal(headers, plain)
	if err != nil {
		return nil, err
	}
	return httptool.HTTPRequest(httptool.HttpMethodPost, callbackURL,
		httptool.WithJsonData(wire),
		httptool.WithHeaders(headers),
		httptool.WithTimeout(timeout))
}

// DecodeResponse 还原银保响应。未命中时原样返回。
// 命中时先 SM4 解密，再用响应头里的 timestamp、nonce、明文和 salt 验 SM3。nonce 必须与本次请求一致。
func (c Config) DecodeResponse(callbackURL string, header http.Header, body, expectNonce string) (string, error) {
	if !c.Applicable(callbackURL) {
		return body, nil
	}
	if err := c.ready(); err != nil {
		return "", err
	}
	if header == nil {
		return "", errors.New("银保回调响应缺少头")
	}
	timestamp := header.Get(headerTimestamp)
	nonce := header.Get(headerNonce)
	sign := header.Get(headerSign)
	systemID := header.Get(headerSystemID)
	if timestamp == "" || nonce == "" || sign == "" || systemID == "" {
		return "", errors.New("银保回调响应缺少 timestamp、nonce、sign 或 systemId")
	}
	if systemID != strings.TrimSpace(c.SystemID) {
		return "", errors.New("银保回调响应 systemId 不匹配")
	}
	if expectNonce != "" && nonce != expectNonce {
		return "", errors.New("银保回调响应 nonce 与请求不一致")
	}
	plain, err := decryptHex([]byte(c.SM4Key), body)
	if err != nil {
		return "", err
	}
	if !signEqual(signSM3(timestamp, nonce, plain, c.SM3Salt), sign) {
		return "", errors.New("银保回调响应签名校验失败")
	}
	return plain, nil
}

func signEqual(expect, actual string) bool {
	a, errA := hex.DecodeString(expect)
	b, errB := hex.DecodeString(actual)
	if errA != nil || errB != nil || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}
