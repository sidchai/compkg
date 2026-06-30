package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// 业务 attribute 白名单 key（RFC §10.2）。
const (
	AttrDeviceNo        = "device_no"
	AttrAccountID       = "account_id"
	AttrMsgID           = "msg_id"
	AttrInstructionID   = "instruction_id"
	AttrInstructionType = "instruction_type"
	AttrRecordID        = "record_id"
	AttrRecordFileName  = "record_file_name"
	AttrRunID           = "run_id"
	AttrJobName         = "job_name"
	AttrTraceOrigin     = "trace_origin"
	AttrTraceOrphan     = "trace_orphan"
	AttrOrphanReason    = "orphan_reason"
	AttrMQClaimResult   = "mq_claim_result"
)

// trace_origin 取值（RFC §10.2）。
const (
	OriginOpenAPI         = "open_api"
	OriginDeviceUpload    = "device_upload"
	OriginDeviceTCPUplink = "device_tcp_uplink"
	OriginSchedulerCron   = "scheduler_cron"
	OriginSchedulerManual = "scheduler_manual"
	OriginCDNCallback     = "cdn_callback"
)

// AttributePolicy 控制 attribute 入 trace 前的清洗策略。
//
//   - DenyKeys：完全拒绝；常见为密钥/token/手机号
//   - HashKeys：以 sha256 摘要前 12 位入 trace；用于需检索但不暴露原值的场景
//   - AllowBizKeys：业务白名单；不在白名单内的 key 仍可入 trace（不强制白名单），
//     但 CI lint 会扫描提示。本 SDK 不做硬拦截，避免引入误伤。
type AttributePolicy struct {
	DenyKeys     map[string]struct{}
	HashKeys     map[string]struct{}
	AllowBizKeys map[string]struct{}
}

// DefaultAttributePolicy 返回 RFC §9 默认配置。
func DefaultAttributePolicy() *AttributePolicy {
	return &AttributePolicy{
		DenyKeys: toSet([]string{
			"phone", "id_card", "email", "password",
			"token", "authorization", "access_token",
			"sign_key", "secret",
			"presigned_url", "callback_url_with_sign",
			"request_body", "response_body",
		}),
		HashKeys: toSet([]string{
			"user_id",
		}),
		AllowBizKeys: toSet([]string{
			AttrDeviceNo, AttrAccountID, AttrMsgID,
			AttrInstructionID, AttrInstructionType,
			AttrRecordID, AttrRecordFileName,
			AttrRunID, AttrJobName,
			AttrTraceOrigin, AttrTraceOrphan, AttrOrphanReason,
			AttrMQClaimResult,
		}),
	}
}

func toSet(keys []string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[strings.ToLower(k)] = struct{}{}
	}
	return m
}

// SetAttributes 安全写入 attribute；nil span 或 disabled 时直接 no-op。
//
// 行为：
//   - DenyKeys 命中：跳过该字段
//   - HashKeys 命中：sha256 前 12 位入 trace
//   - 其他：原值入 trace
func SetAttributes(span oteltrace.Span, kvs ...attribute.KeyValue) {
	if span == nil || !span.IsRecording() {
		return
	}
	policy := currentPolicy.Load()
	if policy == nil {
		span.SetAttributes(kvs...)
		return
	}
	out := kvs[:0:0]
	for _, kv := range kvs {
		key := strings.ToLower(string(kv.Key))
		if _, deny := policy.DenyKeys[key]; deny {
			continue
		}
		if _, hashIt := policy.HashKeys[key]; hashIt {
			out = append(out, attribute.String(string(kv.Key), hashValue(kv.Value.AsString())))
			continue
		}
		out = append(out, kv)
	}
	if len(out) > 0 {
		span.SetAttributes(out...)
	}
}

// SetBizAttributes 一次性写入常见业务 attribute；空字符串/0 跳过。
//
// 用法示例：
//
//	trace.SetBizAttributes(span, trace.BizAttrs{
//	    DeviceNo:    "abc",
//	    AccountID:   "10001",
//	    MsgID:       msgId,
//	    TraceOrigin: trace.OriginOpenAPI,
//	})
type BizAttrs struct {
	DeviceNo        string
	AccountID       string
	MsgID           string
	InstructionID   int64
	InstructionType string
	RecordID        int64
	RecordFileName  string
	RunID           string
	JobName         string
	TraceOrigin     string
	TraceOrphan     bool
	OrphanReason    string
	MQClaimResult   string
}

// SetBizAttributes 把 BizAttrs 写入 span（按非零字段），自动应用 AttributePolicy。
func SetBizAttributes(span oteltrace.Span, b BizAttrs) {
	if span == nil || !span.IsRecording() {
		return
	}
	kvs := make([]attribute.KeyValue, 0, 12)
	if b.DeviceNo != "" {
		kvs = append(kvs, attribute.String(AttrDeviceNo, b.DeviceNo))
	}
	if b.AccountID != "" {
		kvs = append(kvs, attribute.String(AttrAccountID, b.AccountID))
	}
	if b.MsgID != "" {
		kvs = append(kvs, attribute.String(AttrMsgID, b.MsgID))
	}
	if b.InstructionID != 0 {
		kvs = append(kvs, attribute.Int64(AttrInstructionID, b.InstructionID))
	}
	if b.InstructionType != "" {
		kvs = append(kvs, attribute.String(AttrInstructionType, b.InstructionType))
	}
	if b.RecordID != 0 {
		kvs = append(kvs, attribute.Int64(AttrRecordID, b.RecordID))
	}
	if b.RecordFileName != "" {
		kvs = append(kvs, attribute.String(AttrRecordFileName, b.RecordFileName))
	}
	if b.RunID != "" {
		kvs = append(kvs, attribute.String(AttrRunID, b.RunID))
	}
	if b.JobName != "" {
		kvs = append(kvs, attribute.String(AttrJobName, b.JobName))
	}
	if b.TraceOrigin != "" {
		kvs = append(kvs, attribute.String(AttrTraceOrigin, b.TraceOrigin))
	}
	if b.TraceOrphan {
		kvs = append(kvs, attribute.Bool(AttrTraceOrphan, true))
	}
	if b.OrphanReason != "" {
		kvs = append(kvs, attribute.String(AttrOrphanReason, b.OrphanReason))
	}
	if b.MQClaimResult != "" {
		kvs = append(kvs, attribute.String(AttrMQClaimResult, b.MQClaimResult))
	}
	SetAttributes(span, kvs...)
}

func hashValue(v string) string {
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:6]) // 12 hex chars
}
