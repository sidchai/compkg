package cdn

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 哨兵错误
var (
	ErrFileNotFound = errors.New("cdn: file not found")          // /api/sign 返回 404
	ErrQueueFull    = errors.New("cdn: download queue full")     // /api/download 返回 429
	ErrDiskFull     = errors.New("cdn: disk space insufficient") // /api/download 返回 507
	ErrUnauthorized = errors.New("cdn: unauthorized")            // 401
	ErrForbidden    = errors.New("cdn: forbidden")               // 403
	ErrBadRequest   = errors.New("cdn: bad request")             // 400
)

// Options CDN 客户端配置
type Options struct {
	ApiUrl      string        // API 地址，如 https://cdn-server-1.dudutalk.com
	FileUrl     string        // 文件地址，如 https://cdn-files.dudutalk.com
	ApiKey      string        // X-API-Key 认证密钥
	CallbackUrl string        // 下载完成回调地址
	Timeout     time.Duration // HTTP 请求超时时间
}

// Option CDN 配置选项接口
type Option interface {
	Apply(*Options)
}

type funcOption struct {
	f func(options *Options)
}

func (fo *funcOption) Apply(o *Options) {
	fo.f(o)
}

func newFuncOption(f func(options *Options)) *funcOption {
	return &funcOption{f: f}
}

// WithApiUrl 设置 API 地址
func WithApiUrl(apiUrl string) Option {
	return newFuncOption(func(o *Options) {
		o.ApiUrl = apiUrl
	})
}

// WithFileUrl 设置文件访问地址
func WithFileUrl(fileUrl string) Option {
	return newFuncOption(func(o *Options) {
		o.FileUrl = fileUrl
	})
}

// WithApiKey 设置 X-API-Key 认证密钥
func WithApiKey(apiKey string) Option {
	return newFuncOption(func(o *Options) {
		o.ApiKey = apiKey
	})
}

// WithCallbackUrl 设置下载完成回调地址
func WithCallbackUrl(callbackUrl string) Option {
	return newFuncOption(func(o *Options) {
		o.CallbackUrl = callbackUrl
	})
}

// WithTimeout 设置 HTTP 请求超时时间
func WithTimeout(timeout time.Duration) Option {
	return newFuncOption(func(o *Options) {
		o.Timeout = timeout
	})
}

// Client CDN 客户端
type Client struct {
	options    Options
	httpClient *http.Client
}

// NewClient 创建 CDN 客户端
func NewClient(opts ...Option) *Client {
	options := Options{
		Timeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt.Apply(&options)
	}
	return &Client{
		options: options,
		httpClient: &http.Client{
			Timeout: options.Timeout,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ---------- 请求/响应结构体 ----------

// DownloadRequest 发起下载任务请求
type DownloadRequest struct {
	Url           string `json:"url"`                      // 源文件下载地址（COS 签名 URL）
	CallbackUrl   string `json:"callback_url"`             // 下载完成回调地址
	ExpiryMinutes int    `json:"expiry_minutes,omitempty"` // 链接有效期（分钟），0=永久
}

// DownloadResponse 下载任务响应
type DownloadResponse struct {
	TaskId    string `json:"task_id"`    // 任务 ID
	Status    string `json:"status"`     // 状态：queued
	AccessUrl string `json:"access_url"` // 访问链接（此时不可用，需等回调）
	Message   string `json:"message"`    // 描述信息
}

// SignRequest 签名续期请求
type SignRequest struct {
	Path          string `json:"path"`                     // 文件相对路径（不含域名）
	ExpiryMinutes int    `json:"expiry_minutes,omitempty"` // 链接有效期（分钟），0=永久
}

// SignResponse 签名续期响应
type SignResponse struct {
	Status        string `json:"status"`         // ok / error
	AccessUrl     string `json:"access_url"`     // 新的签名链接
	ExpiryMinutes int    `json:"expiry_minutes"` // 有效期
	Message       string `json:"message"`        // 错误信息（404 时）
}

// CallbackPayload CDN 回调请求体
type CallbackPayload struct {
	TaskId         string   `json:"task_id"`         // 任务 ID
	Status         string   `json:"status"`          // success / failed
	AccessUrl      string   `json:"access_url"`      // 文件访问地址（成功时有效）
	FileSize       *int64   `json:"file_size"`       // 文件大小（字节），失败时 null
	ElapsedSeconds *float64 `json:"elapsed_seconds"` // 下载耗时（秒），失败时 null
	Error          *string  `json:"error"`           // 错误信息，成功时 null
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status          string    `json:"status"`           // healthy / degraded / unhealthy
	UptimeSeconds   int64     `json:"uptime_seconds"`   // 运行时长
	SSD             *DiskInfo `json:"ssd"`              // SSD 信息
	HDD             *DiskInfo `json:"hdd"`              // HDD 信息
	ActiveDownloads int       `json:"active_downloads"` // 活跃下载数
}

// DiskInfo 磁盘信息
type DiskInfo struct {
	Ok          bool    `json:"ok"`           // 是否正常
	UsedPercent float64 `json:"used_percent"` // 使用百分比
	TotalFiles  int     `json:"total_files"`  // 文件总数
}

// ---------- API 方法 ----------

// Download 发起异步下载任务（预热）
func (c *Client) Download(cosUrl string, expiryMinutes int) (*DownloadResponse, error) {
	req := &DownloadRequest{
		Url:           cosUrl,
		CallbackUrl:   c.options.CallbackUrl,
		ExpiryMinutes: expiryMinutes,
	}
	var resp DownloadResponse
	if err := c.doPost("/api/download", req, &resp); err != nil {
		return nil, fmt.Errorf("cdn download: %w", err)
	}
	return &resp, nil
}

// Sign 签名续期，返回新的访问链接
// 当文件不存在时返回 ErrFileNotFound
func (c *Client) Sign(cdnPath string, expiryMinutes int) (*SignResponse, error) {
	req := &SignRequest{
		Path:          cdnPath,
		ExpiryMinutes: expiryMinutes,
	}
	var resp SignResponse
	if err := c.doPost("/api/sign", req, &resp); err != nil {
		return nil, fmt.Errorf("cdn sign: %w", err)
	}
	return &resp, nil
}

// Health 健康检查
func (c *Client) Health() (*HealthResponse, error) {
	reqUrl := strings.TrimRight(c.options.ApiUrl, "/") + "/health"
	httpReq, err := http.NewRequest(http.MethodGet, reqUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("cdn health new request: %w", err)
	}
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cdn health request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("cdn health read body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cdn health status %d: %s", httpResp.StatusCode, string(body))
	}

	var resp HealthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("cdn health unmarshal: %w", err)
	}
	return &resp, nil
}

// ---------- 工具方法 ----------

// ExtractCdnPath 从 COS URL 中提取 CDN 相对路径
// 例如: https://dudutalk-xxx.cos.ap-guangzhou.myqcloud.com/audio/2025/04/09/xxx.mp3
// 返回: audio/2025/04/09/xxx.mp3
func ExtractCdnPath(cosUrl string) string {
	u, err := url.Parse(cosUrl)
	if err != nil {
		return ""
	}
	path := strings.TrimPrefix(u.Path, "/")
	return path
}

// ExpiryHoursToMinutes 将小时转换为分钟（用于 AudioFileExpirationTime → expiry_minutes）
func ExpiryHoursToMinutes(hours int) int {
	if hours <= 0 {
		return 0
	}
	return hours * 60
}

// ---------- 内部方法 ----------

// doPost 统一 POST 请求处理
func (c *Client) doPost(path string, reqBody interface{}, respBody interface{}) error {
	reqUrl := strings.TrimRight(c.options.ApiUrl, "/") + path

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, reqUrl, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", c.options.ApiKey)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// 错误码映射（2xx 均视为成功）
	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		// 成功
	} else {
		switch httpResp.StatusCode {
		case http.StatusNotFound:
			return ErrFileNotFound
		case http.StatusBadRequest:
			return fmt.Errorf("%w: %s", ErrBadRequest, string(respBytes))
		case http.StatusUnauthorized:
			return ErrUnauthorized
		case http.StatusForbidden:
			return ErrForbidden
		case http.StatusTooManyRequests:
			return ErrQueueFull
		case http.StatusInsufficientStorage:
			return ErrDiskFull
		default:
			return fmt.Errorf("unexpected status %d: %s", httpResp.StatusCode, string(respBytes))
		}
	}

	if err := json.Unmarshal(respBytes, respBody); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}
