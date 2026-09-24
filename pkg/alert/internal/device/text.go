package device

import "fmt"

// 现象文案必须让值班看懂，禁止写 wait_upload_num 等字段名。阈值与 YAML/默认值同一处来。

func formatTempFire(v, threshold float64) string {
	return fmt.Sprintf("设备连续2次上报温度超过%.0f℃，当前%.0f℃", threshold, v)
}
func formatTempResolve(v float64) string {
	return fmt.Sprintf("设备温度已经恢复正常，当前%.0f℃", v)
}
func formatBatteryJump(prev, cur, jump float64) string {
	return fmt.Sprintf("设备上报的电量变化超过%.0f%%，从%.0f%%变为%.0f%%", jump, prev, cur)
}
func formatBatteryInvalid(cur float64) string {
	return fmt.Sprintf("设备上报的电量为%.0f%%，不在0%%至100%%范围内", cur)
}
func formatSDFire(mb, threshold float64) string {
	return fmt.Sprintf("设备连续2次上报SD卡剩余空间不足%.0fMB，当前%.0fMB", threshold, mb)
}
func formatSDResolve(mb float64) string {
	return fmt.Sprintf("设备SD卡剩余空间已经恢复至%.0fMB", mb)
}
func formatWaitFire(n, threshold float64) string {
	return fmt.Sprintf("设备连续2次上报待上传录音超过%.0f条，当前%.0f条", threshold, n)
}
func formatWaitResolve(n float64) string {
	return fmt.Sprintf("设备待上传录音已经降至%.0f条", n)
}
func formatReconnectFire(n, windowSec, threshold int) string {
	min := windowSec / 60
	if min <= 0 {
		min = windowSec
	}
	return fmt.Sprintf("设备%d分钟内重新联网%d次，告警阈值%d次", min, n, threshold)
}
