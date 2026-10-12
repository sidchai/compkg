package core

import (
	"fmt"
	"time"
)

// 下列 format* 只给设备引擎拼 phenomenon，禁止出现字段名或 stage。
func formatTempFire(v float64) string {
	return fmt.Sprintf("设备连续2次上报温度超过65℃，当前%.0f℃", v)
}
func formatTempResolve(v float64) string {
	return fmt.Sprintf("设备温度已经恢复正常，当前%.0f℃", v)
}
func formatBatteryJump(prev, cur float64) string {
	return fmt.Sprintf("设备上报的电量变化超过30%%，从%.0f%%变为%.0f%%", prev, cur)
}
func formatBatteryInvalid(cur float64) string {
	return fmt.Sprintf("设备上报的电量为%.0f%%，不在0%%至100%%范围内", cur)
}
func formatSDFire(mb float64) string {
	return fmt.Sprintf("设备连续2次上报SD卡剩余空间不足150MB，当前%.0fMB", mb)
}
func formatSDResolve(mb float64) string {
	return fmt.Sprintf("设备SD卡剩余空间已经恢复至%.0fMB", mb)
}
func formatWaitFire(n float64) string {
	return fmt.Sprintf("设备连续2次上报待上传录音超过200条，当前%.0f条", n)
}
func formatWaitResolve(n float64) string {
	return fmt.Sprintf("设备待上传录音已经降至%.0f条", n)
}
func formatReconnectFire(n int) string {
	return fmt.Sprintf("设备30分钟内重新联网%d次，告警阈值5次", n)
}

// HumanDuration 把时长转成“45秒、6分钟、32分钟”，禁止毫秒。
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	min := sec / 60
	if min < 60 {
		return fmt.Sprintf("%d分钟", min)
	}
	return fmt.Sprintf("%d小时%d分钟", min/60, min%60)
}

// RecordPhenomenon 录音四阶段中文现象，禁止输出 stage 等内部词。
func RecordPhenomenon(stage string, resolved bool) string {
	if resolved {
		switch stage {
		case StageCreateTranscodeJob:
			return "录音转码任务已创建"
		case StageTranscode:
			return "录音转码已经完成"
		case StagePlatformReady:
			return "录音现在可以正常播放"
		case StageCallbackDispatch:
			return "系统已经向客户发出录音回调"
		}
	}
	switch stage {
	case StageCreateTranscodeJob:
		return "录音上传完成后，超过15秒仍未创建转码任务"
	case StageTranscode:
		return "录音转码超过30分钟仍未完成"
	case StagePlatformReady:
		return "录音转码完成后，超过15秒仍无法播放"
	case StageCallbackDispatch:
		return "录音已可播放，但系统超过40秒仍未向客户发出回调"
	}
	return "录音处理超时"
}

// LogPhenomenon 日志阶段中文现象。
func LogPhenomenon(resolved bool) string {
	if resolved {
		return "设备日志已经处理完成"
	}
	return "日志文件可读后，超过15秒仍未处理完成"
}

// SceneOf 事件编码对应的值班可读场景。
func SceneOf(code string) string {
	switch code {
	case CodeRecordPipeline:
		return "录音处理"
	case CodeLogPipeline:
		return "日志上传"
	case CodeOpenAPIUnavailable:
		return "开放接口"
	case CodeTCPAbnormal:
		return "TCP接入"
	case CodeInstructionTimeout:
		return "指令下发"
	case CodeDeviceHealth:
		return "设备状态"
	case CodeServiceUnavailable:
		return "服务运行"
	case CodeHostCPU, CodeHostMemory:
		return "服务器资源"
	case CodeStorageCritical:
		return "存储空间"
	case CodeDependencyNetwork:
		return "网络访问"
	case CodeMySQLUnavailable, CodeMySQLConnCapacity, CodeMySQLPerfBlocked:
		return "MySQL"
	case CodeRedisUnavailable, CodeRedisConnCapacity:
		return "Redis"
	case CodeMQService:
		return "MQ服务"
	case CodeMQFlow:
		return "MQ消息流转"
	case EventLegacyNotify:
		return "历史通知"
	default:
		return "系统监控"
	}
}
