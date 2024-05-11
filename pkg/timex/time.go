/**
 * @Description: 时间工具类
 * @Version: 1.0
 * @Author: sidchai
 * @Date: 2022/2/17 16:58
 */

package timex

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UnixNano
//
//	@Description: 返回当前时间的纳秒时间戳
//	@return int64
func UnixNano() int64 {
	return time.Now().UnixNano()
}

// Unix
//
//	@Description: 返回当前时间的秒数时间戳(10位)
//	@return int64
func Unix() int64 {
	return time.Now().Unix()
}

// UnixMilli
//
//	@Description: 返回当前时间的毫秒时间戳(13位)
//	@return int64
func UnixMilli() int64 {
	return time.Now().UnixNano() / 1e6
}

// GetTodayTimeByTimestamp
//
//	@Author: sidchai
//	@Description: 根据秒级时间戳获取当天开始时间时间戳
//	@param timestamp 妙计时间戳
//	@return int64 当天开始时间秒级时间戳
func GetTodayTimeByTimestamp(timestamp int64) int64 {
	curTime := time.Unix(timestamp, 0)
	dayTime := time.Date(curTime.Year(), curTime.Month(), curTime.Day(), 0, 0, 0, 0, time.Local)
	return dayTime.Unix()
}

// GetTodayEndTimeByTimestamp
//
//	@Author: sidchai
//	@Description: 根据秒级时间戳获取当天结束时间时间戳
//	@param timestamp 秒级时间戳
//	@return int64 当天结束时间秒级时间戳
func GetTodayEndTimeByTimestamp(timestamp int64) int64 {
	curTime := time.Unix(timestamp, 0)
	dayTime := time.Date(curTime.Year(), curTime.Month(), curTime.Day(), 23, 59, 59, 0, time.Local)
	return dayTime.Unix()
}

// GetMonthTimeByTimestamp
//
//	@Author: sidchai
//	@Description: 根据秒级时间戳获取当月开始时间时间戳
//	@param timestamp 秒级时间戳
//	@return int64 当月开始时间秒级时间戳
func GetMonthTimeByTimestamp(timestamp int64) int64 {
	curTime := time.Unix(timestamp, 0)
	dayTime := time.Date(curTime.Year(), curTime.Month(), 1, 0, 0, 0, 0, time.Local)
	return dayTime.Unix()
}

// GetTimeDayByTimestamp
//
//	@Author: sidchai
//	@Description: 时间戳格式化(年-月-日)
//	@param timestamp 秒级时间戳
//	@return string 时间格式化
func GetTimeDayByTimestamp(timestamp int64) string {
	timeTemplate := "2006-01-02"
	return time.Unix(timestamp, 0).Format(timeTemplate)
}

// GetTimeByTimestamp
//
//	@Author: sidchai
//	@Description: 时间戳格式化(年-月-日 时:分:秒)
//	@param timestamp 秒级时间戳
//	@return string 时间格式化
func GetTimeByTimestamp(timestamp int64) string {
	timeTemplate := "2006-01-02 15:04:05"
	return time.Unix(timestamp, 0).Format(timeTemplate)
}

// GetTimeByTimestampMilli
//
//	@Description:  时间戳格式化(年-月-日 时:分:秒.毫秒值)
//	@param timestamp 毫秒秒级时间戳
//	@return string 时间格式化
func GetTimeByTimestampMilli(timestamp int64) string {
	timeTemplate := "2006-01-02 15:04:05.000"
	return time.UnixMilli(timestamp).Format(timeTemplate)
}

func GetTimestampByTime(timeFormat string) int64 {
	location, err := time.ParseInLocation("2006-01-02 15:04:05", timeFormat, time.Local)
	if err != nil {
		return 0
	}
	return location.UnixMilli()
}

// TimeFormat
//
//	@Description: 字符串时间转时间戳
//	@param timeFormat 要转时间戳的时间
//	@param sType 格式类型(1:时分秒 2:时分秒毫秒)
//	@return int64
func TimeFormat(timeFormat string, sType int) int64 {
	location := time.Time{}
	switch sType {
	case 1:
		location, _ = time.ParseInLocation("2006-01-02 15:04:05", timeFormat, time.Local)
		return location.Unix()
	case 2:
		location, _ = time.ParseInLocation("2006-01-02 15:04:05.000", timeFormat, time.Local)
		return location.Unix()
	default:
		location, _ = time.ParseInLocation("2006-01-02 15:04:05", timeFormat, time.Local)
		return location.Unix()
	}
}

// SecondsToTimeFormat
//
//	@Description: 秒数转格式化的时分秒
//	@param seconds
//	@return string
func SecondsToTimeFormat(seconds int) string {
	duration := time.Second * time.Duration(seconds)
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds = int(duration.Seconds()) % 60

	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// TimestampFormat
//
//	@Description: 时间戳格式化
//	@param t 时间戳
//	@param sType 格式化类型
//	@return string 格式化结果
func TimestampFormat(t int64, sType int) string {
	if t <= 0 {
		return ""
	}
	if len(fmt.Sprintf("%d", t)) == 10 {
		t = t * 1000
	} else if len(fmt.Sprintf("%d", t)) == 13 {

	} else {
		return ""
	}
	curTime := time.UnixMilli(t)
	switch sType {
	case 1:
		return curTime.Format("2006-01-02 15:04:05.000")
	case 2:
		return curTime.Format("2006-01-02 15:04:05")
	case 3:
		return curTime.Format("2006-01-02 15:04")
	case 4:
		return curTime.Format("2006-01-02 15")
	case 5:
		return curTime.Format("2006-01-02")
	case 6:
		return curTime.Format("2006-01")
	case 7:
		return curTime.Format("2006")
	case 8:
		return curTime.Format("20060102150405")
	}
	return ""
}

// ParseTimeStr
//
//	@Description: 解析时间字符串
//	@param timeStr 时间字符串
//	@return time.Duration 时间类型
//	@return error
func ParseTimeStr(timeStr string) (time.Duration, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format")
	}

	minutes, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}

	secondsWithMillis := parts[1]
	dotIndex := strings.Index(secondsWithMillis, ".")
	if dotIndex == -1 {
		return 0, fmt.Errorf("invalid time format")
	}

	seconds, err := strconv.Atoi(secondsWithMillis[:dotIndex])
	if err != nil {
		return 0, err
	}

	millis, err := strconv.Atoi(secondsWithMillis[dotIndex+1:])
	if err != nil {
		return 0, err
	}

	return time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(millis)*time.Millisecond, nil
}
