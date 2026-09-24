package core

// 18 个 P0 的稳定 eventCode，禁止业务自行改名。
const (
	CodeRecordPipeline     = "record.pipeline_timeout"
	CodeLogPipeline        = "log.pipeline_timeout"
	CodeOpenAPIUnavailable = "openapi.unavailable"
	CodeTCPAbnormal        = "tcp.service_abnormal"
	CodeInstructionTimeout = "instruction.pipeline_timeout"
	CodeDeviceHealth       = "device.health_abnormal"
	CodeServiceUnavailable = "service.unavailable"
	CodeHostCPU            = "host.cpu_critical"
	CodeHostMemory         = "host.memory_low"
	CodeStorageCritical    = "storage.critical"
	CodeDependencyNetwork  = "dependency.network_abnormal"
	CodeMySQLUnavailable   = "mysql.unavailable"
	CodeMySQLConnCapacity  = "mysql.connection_capacity"
	CodeMySQLPerfBlocked   = "mysql.performance_blocked"
	CodeRedisUnavailable   = "redis.unavailable"
	CodeRedisConnCapacity  = "redis.connection_capacity"
	CodeMQService          = "mq.service_abnormal"
	CodeMQFlow             = "mq.flow_abnormal"
)

// 录音四阶段。前一阶段未结束，后一阶段不得凭空计时。
const (
	StageCreateTranscodeJob = "create_transcode_job"
	StageTranscode          = "transcode"
	StagePlatformReady      = "platform_ready"
	StageCallbackDispatch   = "callback_dispatch"
	// StageLogProcess 离线日志：文件可读后计时，Deal 成功删标记。
	StageLogProcess = "log_process"
	// StageInstructionAck 指令创建到成功响应，监控口径 32s，不认 use_status=4。
	StageInstructionAck = "instruction_ack"
)

// 设备健康 subType。同一设备多 subType 可合并一条推送，不同设备不得合并。
const (
	SubTemperatureHigh   = "temperature_high"
	SubBatteryAbnormal   = "battery_abnormal"
	SubReconnectFrequent = "reconnect_frequent"
	SubSDCardLow         = "sdcard_low"
	SubUploadBacklog     = "upload_backlog"
)

// MQ 流转 stage。四条件为 OR，同聚合对象只发一条 mq.flow_abnormal。
const (
	StageProducerPending = "producer_pending"
	StageConsumerOffline = "consumer_offline"
	StageConsumerStalled = "consumer_stalled"
	StageMessageDelayed  = "message_delayed"
)
