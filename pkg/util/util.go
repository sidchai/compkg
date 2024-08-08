package util

import (
	"crypto/md5"
	"fmt"
	"github.com/google/uuid"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"time"
)

var (
	// DefaultMaxVoltage 默认最高电压(挂牌)
	DefaultMaxVoltage int64 = 4150
	// DefaultMaxVoltageX 默认最高电压(胸牌)
	DefaultMaxVoltageX int64 = 4050
	// DefaultMinVoltage 默认最低电压
	DefaultMinVoltage int64 = 3450
	//// DefaultMinVoltageX 默认最低电压(胸牌)
	//DefaultMinVoltageX int64 = 3450
)

// Md5
//
//	@Description: md5小写加密
//	@param s 带加密的字符串
//	@return string 加密后的数据
func Md5(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}

// DivideAndRoundUp
//
//	@Description: 计算总页数
//	@param dividend 总数
//	@param divisor 每页数量
//	@return int 总页数
func DivideAndRoundUp(dividend, divisor int64) int64 {
	// 使用浮点数进行除法操作
	quotient := float64(dividend) / float64(divisor)

	// 使用 math.Ceil() 函数进行向上取整
	roundedUp := int64(math.Ceil(quotient))

	return roundedUp
}

func GetUUID() string {
	ui, _ := uuid.NewUUID()
	return ui.String()
}

// GetTraceId
//
//	@Description: 获取16位随机字符串
//	@return string
func GetTraceId() string {
	str := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := []byte(str)
	bytesLen := len(b)
	var result []byte
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 16; i++ {
		result = append(result, b[r.Intn(bytesLen)])
	}
	return string(result)
}

func StructToMap(obj interface{}) map[string]interface{} {
	obj1 := reflect.TypeOf(obj)
	obj2 := reflect.ValueOf(obj)
	var data = make(map[string]interface{})
	for i := 0; i < obj1.NumField(); i++ {
		// 内嵌结构体就跳过
		if obj1.Field(i).Type.Kind() == reflect.Struct {
			continue
		}
		data[obj1.Field(i).Name] = obj2.Field(i).Interface()
	}
	return data
}

func DeviceVoltageCalculate(batteryVoltage int, deviceType string) int64 {
	maxVoltage := DefaultMaxVoltage
	if strings.HasSuffix(deviceType, "X") {
		maxVoltage = DefaultMaxVoltageX
	}
	var remainPower int64
	if int64(batteryVoltage) > maxVoltage {
		remainPower = 100
	} else if int64(batteryVoltage) <= DefaultMinVoltage {
		remainPower = 1
	} else {
		remainPower = 100 - int64(math.Ceil(float64(maxVoltage-int64(batteryVoltage))/float64(maxVoltage-DefaultMinVoltage)*10)*10)
		if remainPower == 0 {
			remainPower = 1
		}
	}
	return remainPower
}

func DeviceStorageCalculate(fullStorage, storage int) int {
	return int(float64(fullStorage) / float64(storage) * 100)
}

// IsInArray 判断元素是否在一个数组里面
func IsInArray[T comparable](target T, arr []T) bool {
	for _, item := range arr {
		if item == target {
			return true
		}
	}
	return false
}

// RemoveQuotes
//
//	@Description: 删除字符串中双引号
func RemoveQuotes(input string) string {
	return strings.ReplaceAll(input, "\"", "")
}

// CheckFolder
//
//	@Description: 检查文件目录是否存在
//	@param path 目录路径
//	@return bool 是否存在
func CheckFolder(path string) bool {
	_, _err := os.Stat(path)
	if _err == nil {
		return true
	}
	if os.IsNotExist(_err) {
		return false
	}
	return false
}
