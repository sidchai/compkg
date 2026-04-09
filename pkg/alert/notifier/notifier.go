package notifier

import (
	"github.com/sidchai/compkg/pkg/alert"
)

// Notifier 通知发送接口（与 alert.Notifier 保持一致）
type Notifier interface {
	Send(event alert.AlertEvent) error
}
