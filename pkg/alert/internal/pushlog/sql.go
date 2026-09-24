package pushlog

import (
	"context"
	"strings"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
)

// TableName 五服务同一张流水表，各连自己的业务库。
const TableName = "iot_p0_alert_push"

const createSQL = `CREATE TABLE IF NOT EXISTS ` + "`iot_p0_alert_push`" + ` (
  ` + "`id`" + ` bigint NOT NULL AUTO_INCREMENT COMMENT '主键',
  ` + "`event_id`" + ` varchar(64) NOT NULL DEFAULT '' COMMENT '推送幂等ID',
  ` + "`event_code`" + ` varchar(64) NOT NULL DEFAULT '' COMMENT '场景编码',
  ` + "`level`" + ` varchar(8) NOT NULL DEFAULT 'P0' COMMENT '级别',
  ` + "`status`" + ` varchar(16) NOT NULL DEFAULT '' COMMENT 'FIRING/RESOLVED',
  ` + "`scene`" + ` varchar(128) NOT NULL DEFAULT '' COMMENT '值班场景',
  ` + "`phenomenon`" + ` text COMMENT '现象原文',
  ` + "`occurred_at`" + ` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '发生时间',
  ` + "`environment`" + ` varchar(64) NOT NULL DEFAULT '' COMMENT '环境',
  ` + "`service`" + ` varchar(64) NOT NULL DEFAULT '' COMMENT '服务名',
  ` + "`object`" + ` varchar(512) NOT NULL DEFAULT '' COMMENT '告警对象明文',
  ` + "`stage`" + ` varchar(64) NOT NULL DEFAULT '' COMMENT '阶段',
  ` + "`is_send_ok`" + ` tinyint NOT NULL DEFAULT 0 COMMENT '出口是否发送成功',
  ` + "`send_err`" + ` varchar(512) NOT NULL DEFAULT '' COMMENT '发送失败原因',
  ` + "`created_at`" + ` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '写入时间',
  ` + "`updated_at`" + ` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (` + "`id`" + `),
  UNIQUE KEY ` + "`uk_event_id`" + ` (` + "`event_id`" + `),
  KEY ` + "`idx_occurred_at`" + ` (` + "`occurred_at`" + `),
  KEY ` + "`idx_service_code`" + ` (` + "`service`" + `,` + "`event_code`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='P0告警推送流水'`

const upsertSQL = `INSERT INTO ` + "`iot_p0_alert_push`" + ` (
  event_id, event_code, level, status, scene, phenomenon, occurred_at,
  environment, service, object, stage, is_send_ok, send_err, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE
  is_send_ok=VALUES(is_send_ok),
  send_err=VALUES(send_err),
  updated_at=VALUES(updated_at)`

// SQLStore 用参数化 SQL 写流水。表不存在时 Ensure 一次，避免启动强依赖迁移权限。
type SQLStore struct {
	exec core.ExecFunc
}

// NewSQL 执行器为空则返回 nil，调用方应跳过绑定。
func NewSQL(exec core.ExecFunc) *SQLStore {
	if exec == nil {
		return nil
	}
	return &SQLStore{exec: exec}
}

// EnsureTable 建表失败只返回 error，不得让进程拒启。
func (s *SQLStore) EnsureTable(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.exec(ctx, createSQL)
}

// Save 按 event_id 幂等。超时由调用方 ctx 控制，默认应 ≤2s。
func (s *SQLStore) Save(ctx context.Context, ev core.Event, sendOK bool, sendErr string) error {
	if s == nil {
		return nil
	}
	now := time.Now()
	occurred := now
	if t, err := time.Parse(time.RFC3339, ev.OccurredAt); err == nil {
		occurred = t
	}
	ok := 0
	if sendOK {
		ok = 1
	}
	return s.exec(ctx, upsertSQL,
		ev.EventID,
		ev.EventCode,
		nz(ev.Level, "P0"),
		ev.Status,
		clip(ev.Scene, 128),
		ev.Phenomenon,
		occurred,
		clip(ev.Environment, 64),
		clip(ev.Service, 64),
		clip(ev.Object, 512),
		clip(ev.Stage, 64),
		ok,
		clip(sendErr, 512),
		now,
		now,
	)
}

func nz(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if n <= 0 || len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
