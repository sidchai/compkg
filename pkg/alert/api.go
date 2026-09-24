package alert

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/sidchai/compkg/pkg/alert/internal/core"
	"github.com/sidchai/compkg/pkg/alert/internal/device"
	"github.com/sidchai/compkg/pkg/alert/internal/engine"
	"github.com/sidchai/compkg/pkg/alert/internal/ingest"
	"github.com/sidchai/compkg/pkg/alert/internal/pipeline"
	"github.com/sidchai/compkg/pkg/alert/internal/pushlog"
	"github.com/sidchai/compkg/pkg/alert/internal/reporter"
	"github.com/sidchai/compkg/pkg/alert/internal/sender"
	"github.com/sidchai/compkg/pkg/alert/internal/store"
)

// 配置与出口模式。业务只改 YAML/环境变量，不直接拼 Sender。
type (
	Config         = core.Config
	WebhookConfig  = core.WebhookConfig
	DingTalkConfig = core.DingTalkConfig
	P0Thresholds   = core.P0Thresholds
)

const (
	// ModeWebhook 生产唯一启用的出口。
	ModeWebhook = core.ModeWebhook
	// ModeDingTalk 库内保留实现，本期不初始化。
	ModeDingTalk = core.ModeDingTalk
)

var (
	// ResolveEnv 解析 ${ENV}，避免密钥进仓库。
	ResolveEnv = core.ResolveEnv
)

// Event 对外告警事件，与文档 6.3 JSON 字段对齐。
type Event = core.Event

const (
	StatusFiring      = core.StatusFiring
	StatusResolved    = core.StatusResolved
	LevelP0           = core.LevelP0
	EventLegacyNotify = core.EventLegacyNotify

	CodeRecordPipeline     = core.CodeRecordPipeline
	CodeLogPipeline        = core.CodeLogPipeline
	CodeOpenAPIUnavailable = core.CodeOpenAPIUnavailable
	CodeTCPAbnormal        = core.CodeTCPAbnormal
	CodeInstructionTimeout = core.CodeInstructionTimeout
	CodeDeviceHealth       = core.CodeDeviceHealth
	CodeServiceUnavailable = core.CodeServiceUnavailable
	CodeHostCPU            = core.CodeHostCPU
	CodeHostMemory         = core.CodeHostMemory
	CodeStorageCritical    = core.CodeStorageCritical
	CodeDependencyNetwork  = core.CodeDependencyNetwork
	CodeMySQLUnavailable   = core.CodeMySQLUnavailable
	CodeMySQLConnCapacity  = core.CodeMySQLConnCapacity
	CodeMySQLPerfBlocked   = core.CodeMySQLPerfBlocked
	CodeRedisUnavailable   = core.CodeRedisUnavailable
	CodeRedisConnCapacity  = core.CodeRedisConnCapacity
	CodeMQService          = core.CodeMQService
	CodeMQFlow             = core.CodeMQFlow

	StageCreateTranscodeJob = core.StageCreateTranscodeJob
	StageTranscode          = core.StageTranscode
	StagePlatformReady      = core.StagePlatformReady
	StageCallbackDispatch   = core.StageCallbackDispatch
	StageLogProcess         = core.StageLogProcess
	StageInstructionAck     = core.StageInstructionAck

	SubTemperatureHigh   = core.SubTemperatureHigh
	SubBatteryAbnormal   = core.SubBatteryAbnormal
	SubReconnectFrequent = core.SubReconnectFrequent
	SubSDCardLow         = core.SubSDCardLow
	SubUploadBacklog     = core.SubUploadBacklog

	StageProducerPending = core.StageProducerPending
	StageConsumerOffline = core.StageConsumerOffline
	StageConsumerStalled = core.StageConsumerStalled
	StageMessageDelayed  = core.StageMessageDelayed
)

var (
	NewEvent          = core.NewEvent
	Dedup             = core.Dedup
	HashID            = core.HashID
	MaskObject        = core.MaskObject
	HumanDuration     = core.HumanDuration
	RecordPhenomenon  = core.RecordPhenomenon
	LogPhenomenon     = core.LogPhenomenon
	RecordKey         = core.RecordKey
	RecordKeyVariants = core.RecordKeyVariants
	SceneOf           = core.SceneOf
	CPUAbnormal       = core.CPUAbnormal
	MemAbnormal       = core.MemAbnormal
	DiskAbnormal      = core.DiskAbnormal
	DiskRecover       = core.DiskRecover
)

// 端口定义在门面，实现藏在 internal，五服务禁止 import internal。
type (
	Sender          = core.Sender
	NopSender       = core.NopSender
	StateStore      = core.StateStore
	ZMember         = core.ZMember
	PipelineTracker = core.PipelineTracker
	ConfirmFunc     = core.ConfirmFunc
	PendingItem     = core.PendingItem
	StageTimeout    = core.StageTimeout
	Sample          = core.Sample
	SampleIngester  = core.SampleIngester
	HostSnapshot    = core.HostSnapshot
	DiskSample      = core.DiskSample
	HostCollector   = core.HostCollector
	PushLogStore    = core.PushLogStore
	ExecFunc        = core.ExecFunc
)

var (
	NewSQLPushLog = pushlog.NewSQL
)

var (
	ErrStoreUnavailable  = core.ErrStoreUnavailable
	ErrCapacityExceeded  = core.ErrCapacityExceeded
	DefaultStageTimeouts = core.DefaultStageTimeouts
)

// 规则引擎对外类型。连续次数与持续时间只填需要的一侧。
type (
	State                 = engine.State
	Action                = engine.Action
	RuleSpec              = engine.RuleSpec
	Evaluator             = engine.Evaluator
	Suppressor            = engine.Suppressor
	InstructionEvent      = engine.InstructionEvent
	InstructionAggregator = engine.InstructionAggregator
)

const (
	StateNormal   = engine.StateNormal
	StatePending  = engine.StatePending
	StateFiring   = engine.StateFiring
	StateResolved = engine.StateResolved
	ActionNone    = engine.ActionNone
	ActionFire    = engine.ActionFire
	ActionRemind  = engine.ActionRemind
	ActionResolve = engine.ActionResolve
)

var (
	NewEvaluator             = engine.NewEvaluator
	NewSuppressor            = engine.NewSuppressor
	MergeOpenAPIIntoService  = engine.MergeOpenAPIIntoService
	NewInstructionAggregator = engine.NewInstructionAggregator
)

// 适配器对外别名。生产装配请走 New(cfg)，下列构造函数主要给单测。
type (
	MemoryStore     = store.MemoryStore
	BoundedIngester = ingest.Bounded
	DeviceEngine    = device.Engine
	DeviceSample    = device.Sample
	DeviceResult    = device.Result
	Reporter        = reporter.Reporter
	WebhookSender   = sender.Webhook
	DingTalkSender  = sender.DingTalk
)

var (
	NewMemoryStore      = store.NewMemoryStore
	NewBoundedIngester  = ingest.New
	NewDeviceEngine     = device.New
	NewReporter         = reporter.New
	QueueDropTotal      = reporter.QueueDropTotal
	ResetQueueDropTotal = reporter.ResetQueueDropTotal
	Sign                = sender.Sign
	FormatDingTalk      = sender.Format
	OutboundEvent       = sender.OutboundEvent
)

const (
	// HeaderTimestamp / HeaderSignature 供接收端验签，禁止当业务标签。
	HeaderTimestamp = sender.HeaderTimestamp
	HeaderSignature = sender.HeaderSignature
)

// NewPipeline 创建阶段跟踪器。标记写失败不得改变业务结果。
func NewPipeline(st StateStore, logger *slog.Logger) PipelineTracker {
	return pipeline.New(st, logger)
}

// NewWebhookSender 仅供单测或自定义装配；生产走 New(cfg)。
func NewWebhookSender(url, secret string, timeout time.Duration, client *http.Client) *WebhookSender {
	return sender.NewWebhook(url, secret, timeout, client)
}

// NewDingTalkSender 仅 mode=dingtalk 时使用，与 Webhook 互斥。
func NewDingTalkSender(rawURL, secret string, timeout time.Duration, client *http.Client) *DingTalkSender {
	return sender.NewDingTalk(rawURL, secret, timeout, client)
}
