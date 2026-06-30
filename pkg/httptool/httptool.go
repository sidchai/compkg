package httptool

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// otelTransport 包装默认 transport，自动注入 W3C traceparent 并生成 client span。
// RFC-07：trace 未 Bootstrap 时 otelhttp 走 no-op，业务零成本。
// 注意：不复用 HttpRequest 内部带 TLSClientConfig 的 transport，因为 HttpRequestCtx 默认共用
// 单例 transport 提升复用率；调用方需要禁用证书校验时应自行构造 *http.Client 调用底层 net/http。
var otelTransport http.RoundTripper = otelhttp.NewTransport(
	&http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: false,
	},
	otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		return "http.client." + r.Method + " " + r.URL.Host
	}),
)

const (
	HttpMethodGet  = "GET"
	HttpMethodPost = "POST"

	Json = "application/json"
	Form = "application/x-www-form-urlencoded"
)

// requestOption
//
//	@Description: 请求参数
type requestOption struct {
	timeout time.Duration     // 超时时间
	data    string            // 参数内容
	headers map[string]string // 请求头
}

func DefaultHeaders() map[string]string {
	return map[string]string{"Content-Type": Json}
}

type Option func(*requestOption)

// 默认请求参数
func defaultRequestOptions() *requestOption {
	return &requestOption{
		timeout: time.Second * 5,
		data:    "",
		headers: make(map[string]string),
	}
}

func HttpRequest(method, url string, options ...Option) (result string, err error) {
	start := time.Now()
	responseHeader := make(map[string][]string)
	defaultOpts := defaultRequestOptions()
	// 记录请求日志
	defer func() {
		dur := int64(time.Since(start) / time.Millisecond)
		fmt.Printf(fmt.Sprintf("http:%s\t, url:%s\t, request_headers:%v\t, in:%s\t, out:%s\t, response_headers:%v\t, err:%v, dur/ms:%v\n", method, url, defaultOpts.headers, defaultOpts.data, result, responseHeader, err, dur))
	}()
	for _, apply := range options {
		apply(defaultOpts)
	}
	// 创建请求对象
	req, err := http.NewRequest(method, url, strings.NewReader(defaultOpts.data))
	if err != nil {
		return
	}
	defer req.Body.Close()
	// 设置请求头
	if len(defaultOpts.headers) != 0 {
		for key, value := range defaultOpts.headers {
			req.Header.Add(key, value)
		}
	}
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // 禁用证书验证
	}
	client := &http.Client{
		Timeout: defaultOpts.timeout,
		Transport: &http.Transport{
			TLSClientConfig:   tlsConfig,
			DisableKeepAlives: true,
		},
	}
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	responseHeader = response.Header
	// 解析响应
	readAll, err := io.ReadAll(response.Body)
	if err != nil {
		return
	}
	result = string(readAll)
	return
}

// HttpRequestCtx 是 HttpRequest 的带 ctx 版本（RFC-07）。
//
// 行为与 HttpRequest 一致，差异：
//  1. 使用 http.NewRequestWithContext 让 otelhttp transport 能拿到 SpanContext
//  2. Transport 替换为 otelhttp，自动注入 W3C traceparent 并产生 client span
//  3. ctx 取消 / 超时会传导给 HTTP 请求，避免长尾连接
//
// 调用方持有请求 ctx（Fiber/Hertz handler、定时任务、消费者）时优先使用此函数，
// 让 trace 链路覆盖所有外部接口调用。
func HttpRequestCtx(ctx context.Context, method, url string, options ...Option) (result string, err error) {
	start := time.Now()
	responseHeader := make(map[string][]string)
	defaultOpts := defaultRequestOptions()
	// 走 compkg/pkg/logger 的 ctx 版本：该日志包会从 ctx 抽取 trace_id/request_id 并写入结构化字段，
	// 各服务只需在启动时调用 logger.SetLogger(...) 注入自己的 zap driver 即可联调。
	defer func() {
		dur := int64(time.Since(start) / time.Millisecond)
		if err != nil {
			logger.CtxErrorf(ctx, "[httptool] http=%s url=%s req_headers=%v in=%s out=%s resp_headers=%v err=%v dur_ms=%d", method, url, defaultOpts.headers, defaultOpts.data, result, responseHeader, err, dur)
		} else {
			logger.CtxInfof(ctx, "[httptool] http=%s url=%s req_headers=%v in=%s out=%s resp_headers=%v dur_ms=%d", method, url, defaultOpts.headers, defaultOpts.data, result, responseHeader, dur)
		}
	}()
	for _, apply := range options {
		apply(defaultOpts)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(defaultOpts.data))
	if err != nil {
		return
	}
	defer req.Body.Close()
	if len(defaultOpts.headers) != 0 {
		for key, value := range defaultOpts.headers {
			req.Header.Add(key, value)
		}
	}
	client := &http.Client{
		Timeout:   defaultOpts.timeout,
		Transport: otelTransport,
	}
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	responseHeader = response.Header
	readAll, err := io.ReadAll(response.Body)
	if err != nil {
		return
	}
	result = string(readAll)
	return
}

func HTTPRequest(method, url string, options ...Option) (response *http.Response, err error) {
	defaultOpts := defaultRequestOptions()
	responseHeader := make(map[string][]string)
	// 记录请求日志
	defer func() {
		fmt.Printf("http:%s, url:%s, in:%s, response_headers:%v\t, err:%v\n", method, url, defaultOpts.data, responseHeader, err)
	}()
	for _, apply := range options {
		apply(defaultOpts)
	}
	// 创建请求对象
	req, err := http.NewRequest(method, url, strings.NewReader(defaultOpts.data))
	if err != nil {
		return
	}
	defer req.Body.Close()
	// 设置请求头
	if len(defaultOpts.headers) != 0 {
		for key, value := range defaultOpts.headers {
			req.Header.Add(key, value)
		}
	}
	client := &http.Client{Timeout: defaultOpts.timeout}
	response, err = client.Do(req)
	if err != nil {
		return
	}
	responseHeader = response.Header
	return
}

// HTTPRequestCtx 是 HTTPRequest 的带 ctx 版本（RFC-07），返回 *http.Response 由调用方自行 Close。
func HTTPRequestCtx(ctx context.Context, method, url string, options ...Option) (response *http.Response, err error) {
	defaultOpts := defaultRequestOptions()
	responseHeader := make(map[string][]string)
	defer func() {
		if err != nil {
			logger.CtxErrorf(ctx, "[httptool] http=%s url=%s in=%s resp_headers=%v err=%v", method, url, defaultOpts.data, responseHeader, err)
		} else {
			logger.CtxInfof(ctx, "[httptool] http=%s url=%s in=%s resp_headers=%v", method, url, defaultOpts.data, responseHeader)
		}
	}()
	for _, apply := range options {
		apply(defaultOpts)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(defaultOpts.data))
	if err != nil {
		return
	}
	defer req.Body.Close()
	if len(defaultOpts.headers) != 0 {
		for key, value := range defaultOpts.headers {
			req.Header.Add(key, value)
		}
	}
	client := &http.Client{Timeout: defaultOpts.timeout, Transport: otelTransport}
	response, err = client.Do(req)
	if err != nil {
		return
	}
	responseHeader = response.Header
	return
}

// WithTimeout 设置过期时间
func WithTimeout(timeout time.Duration) Option {
	return func(opts *requestOption) {
		opts.timeout = timeout
	}
}

// WithHeaders 设置请求头
func WithHeaders(headers map[string]string) Option {
	return func(opts *requestOption) {
		for k, v := range headers {
			opts.headers[k] = v
		}
		return
	}
}

// WithData 设置请求参数
func WithData(data interface{}) Option {
	return func(opts *requestOption) {
		marshal, _ := json.Marshal(data)
		opts.data = string(marshal)
	}
}

func WithJsonData(data string) Option {
	return func(opts *requestOption) {
		opts.data = data
	}
}
