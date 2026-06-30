// Package tracelegacy 保留 RFC-07 之前的旧 trace 数据结构，仅供 compkg/pkg/cache
// 等历史模块继续使用。新代码请使用 compkg/pkg/trace（OpenTelemetry SDK）。
//
// 迁移背景：
//   - RFC-07 决定 compkg/pkg/trace 包名让位给 OTel 标准 SDK
//   - 旧 trace 结构体（Trace/Request/Response/Dialog/SQL/Cache/Debug）只在
//     compkg/pkg/cache 内部使用，不属于跨服务追踪体系
//   - 暂不重构 cache 的 trace 字段，统一改 import 路径即可
package tracelegacy

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"sync"

	"go.uber.org/zap"
)

const Header = "TRACE-ID"

type T interface {
	ID() string
	WithRequest(req *Request) *Trace
	WithResponse(resp *Response) *Trace
	AppendDialog(dialog *Dialog) *Trace
	AppendSQL(sql *SQL) *Trace
	AppendCache(Cache *Cache) *Trace
	SetLogger(logger *zap.Logger)
	SetAlwaysTrace(b bool)
}

// Trace 记录的参数
type Trace struct {
	mux                sync.Mutex
	Identifier         string      `json:"trace_id"`
	Request            *Request    `json:"request"`
	Response           *Response   `json:"response"`
	ThirdPartyRequests []*Dialog   `json:"third_party_requests"`
	Debugs             []*Debug    `json:"debugs"`
	SQLs               []*SQL      `json:"sqls"`
	Cache              []*Cache    `json:"Cache"`
	Success            bool        `json:"success"`
	CostMillisecond    float64     `json:"cost_millisecond"`
	Logger             *zap.Logger `json:"-"`
	AlwaysTrace        bool        `json:"always_trace"`
}

// Request 请求信息
type Request struct {
	TTL         string      `json:"ttl"`
	Method      string      `json:"method"`
	DecodedURL  string      `json:"decoded_url"`
	Header      interface{} `json:"header"`
	Body        interface{} `json:"body"`
	Logger      *zap.Logger `json:"-"`
	AlwaysTrace bool        `json:"always_trace"`
}

// Response 响应信息
type Response struct {
	Header          interface{} `json:"header"`
	Body            interface{} `json:"body"`
	BusinessCode    int         `json:"business_code,omitempty"`
	BusinessCodeMsg string      `json:"business_code_msg,omitempty"`
	HttpCode        int         `json:"http_code"`
	HttpCodeMsg     string      `json:"http_code_msg"`
	CostMillisecond int64       `json:"cost_millisecond"`
	Logger          *zap.Logger `json:"-"`
	AlwaysTrace     bool        `json:"always_trace"`
}

type SQL struct {
	TraceTime             string      `json:"trace_time"`
	Stack                 string      `json:"stack"`
	SQL                   string      `json:"sql"`
	AffectedRows          int64       `json:"affected_rows"`
	CostMillisecond       int64       `json:"cost_millisecond"`
	SlowLoggerMillisecond int64       `json:"slow_logger_millisecond"`
	Logger                *zap.Logger `json:"-"`
	AlwaysTrace           bool        `json:"always_trace"`
}

type Cache struct {
	Name                  string      `json:"name"`
	TraceTime             string      `json:"trace_time"`
	CMD                   string      `json:"cmd"`
	Key                   string      `json:"key"`
	Value                 interface{} `json:"value,omitempty"`
	TTL                   float64     `json:"ttl,omitempty"`
	CostMillisecond       int64       `json:"cost_millisecond"`
	SlowLoggerMillisecond int64       `json:"slow_logger_millisecond"`
	Logger                *zap.Logger `json:"-"`
	AlwaysTrace           bool        `json:"always_trace"`
}

type D interface {
	AppendResponse(resp *Response)
}

// Dialog 内部调用其它方接口的会话信息；失败时会有 retry，所以 response 会有多次。
type Dialog struct {
	mux             sync.Mutex
	Request         *Request    `json:"request"`
	Responses       []*Response `json:"responses"`
	Success         bool        `json:"success"`
	CostMillisecond int64       `json:"cost_millisecond"`
	Logger          *zap.Logger `json:"-"`
	AlwaysTrace     bool        `json:"always_trace"`
}

// AppendResponse 按转的追加 response 信息
func (d *Dialog) AppendResponse(resp *Response) {
	if resp == nil {
		return
	}

	d.mux.Lock()
	d.Responses = append(d.Responses, resp)
	d.mux.Unlock()
}

// Debug 自定义调试信息
type Debug struct {
	Key             string      `json:"key"`
	Value           interface{} `json:"value"`
	CostMillisecond int64       `json:"cost_millisecond"`
	Logger          *zap.Logger `json:"-"`
}

func New(id string) *Trace {
	if id == "" {
		buf := make([]byte, 10)
		_, _ = io.ReadFull(rand.Reader, buf)
		id = hex.EncodeToString(buf)
	}
	return &Trace{Identifier: id}
}

func (t *Trace) ID() string { return t.Identifier }

func (t *Trace) WithRequest(req *Request) *Trace {
	t.Request = req
	return t
}

func (t *Trace) WithResponse(resp *Response) *Trace {
	t.Response = resp
	return t
}

func (t *Trace) AppendDialog(dialog *Dialog) *Trace {
	if dialog == nil {
		return t
	}
	t.mux.Lock()
	defer t.mux.Unlock()
	t.ThirdPartyRequests = append(t.ThirdPartyRequests, dialog)
	return t
}

func (t *Trace) AppendDebug(debug *Debug) *Trace {
	if debug == nil {
		return t
	}
	t.mux.Lock()
	defer t.mux.Unlock()
	t.Debugs = append(t.Debugs, debug)
	return t
}

func (t *Trace) AppendSQL(sql *SQL) *Trace {
	if sql == nil {
		return t
	}
	t.mux.Lock()
	defer t.mux.Unlock()
	t.SQLs = append(t.SQLs, sql)
	return t
}

func (t *Trace) SetLogger(logger *zap.Logger) { t.Logger = logger }

func (t *Trace) SetAlwaysTrace(b bool) { t.AlwaysTrace = b }

func (t *Trace) AppendCache(c *Cache) *Trace {
	if c == nil {
		return t
	}
	t.mux.Lock()
	defer t.mux.Unlock()
	t.Cache = append(t.Cache, c)
	return t
}
