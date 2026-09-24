package engine

import (
	"sync"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// codePriority 数字越小越优先。主机/磁盘挂了时业务链路不再单独首告。
var codePriority = map[string]int{
	core.CodeServiceUnavailable: 10,
	core.CodeStorageCritical:    20,
	core.CodeMySQLUnavailable:   30,
	core.CodeRedisUnavailable:   30,
	core.CodeMQService:          30,
	core.CodeTCPAbnormal:        40,
	core.CodeOpenAPIUnavailable: 45,
	core.CodeRecordPipeline:     50,
	core.CodeLogPipeline:        50,
	core.CodeInstructionTimeout: 50,
	core.CodeMQFlow:             55,
	core.CodeDependencyNetwork:  60,
}

// Suppressor 同一故障只发主告警。
type Suppressor struct {
	mu     sync.Mutex
	active map[string]suppressItem
}

type suppressItem struct {
	code    string
	until   time.Time
	details []string
}

// NewSuppressor 创建域级抑制器。domain 建议用 host= / mysql= 等粗粒度键。
func NewSuppressor() *Suppressor {
	return &Suppressor{active: make(map[string]suppressItem)}
}

// ShouldSend 窗口内已有更高优先级主告警则返回 false，避免同一故障拆成多条。
func (s *Suppressor) ShouldSend(domain, code string, now time.Time) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.active[domain]
	if ok && now.After(cur.until) {
		delete(s.active, domain)
		ok = false
	}
	p := priorityOf(code)
	if !ok {
		s.active[domain] = suppressItem{code: code, until: now.Add(10 * time.Minute)}
		return true
	}
	cp := priorityOf(cur.code)
	if p < cp {
		s.active[domain] = suppressItem{code: code, until: now.Add(10 * time.Minute)}
		return true
	}
	if p > cp {
		return false
	}
	return cur.code != code
}

func (s *Suppressor) Clear(domain string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, domain)
}

func priorityOf(code string) int {
	if p, ok := codePriority[code]; ok {
		return p
	}
	return 80
}

// MergeOpenAPIIntoService 实例已 service.unavailable 时，openapi 只并入详情不单独推。
func MergeOpenAPIIntoService(serviceDown bool) bool {
	return !serviceDown
}
