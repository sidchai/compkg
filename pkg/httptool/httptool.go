package httptool

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
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
	defaultOpts := defaultRequestOptions()
	// 记录请求日志
	defer func() {
		dur := int64(time.Since(start) / time.Millisecond)
		fmt.Printf(fmt.Sprintf("http:%s, url:%s, in:%s, out:%s, err:%v, dur/ms:%v\n", method, url, defaultOpts.data, result, err, dur))
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
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	// 解析响应
	readAll, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return
	}
	result = string(readAll)
	return
}

func HTTPRequest(method, url string, options ...Option) (response *http.Response, err error) {
	defaultOpts := defaultRequestOptions()
	// 记录请求日志
	defer func() {
		fmt.Sprintf(fmt.Sprintf("http:%s, url:%s, in:%s, err:%v\n", method, url, defaultOpts.data, err))
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
