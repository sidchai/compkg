package util

import (
	"crypto/md5"
	"fmt"
	"github.com/google/uuid"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"
)

var (
	// DefaultMaxVoltage 默认最高电压(挂牌)
	DefaultMaxVoltage int64 = 4150
	// DefaultMaxVoltageX 默认最高电压(胸牌: 8-30前)
	DefaultMaxVoltageX int64 = 4050
	// DefaultMaxVoltageNX 默认最高电压(胸牌: 8-30后)
	DefaultMaxVoltageNX int64 = 4150
	// DefaultMinVoltage 默认最低电压
	DefaultMinVoltage int64 = 3450
	// DefaultMinVoltageX 默认最低电压(胸牌)
	DefaultMinVoltageX int64 = 3350
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

type VoltageCalculate struct {
	BatteryVoltage int
	DeviceType     string
	DensityType    int
	AccountId      int64
}

func DeviceVoltageCalculate(in *VoltageCalculate) int64 {
	maxVoltage := DefaultMaxVoltage
	minVoltage := DefaultMinVoltage
	if strings.HasSuffix(in.DeviceType, "X") {
		maxVoltage = DefaultMaxVoltageX
		minVoltage = DefaultMinVoltageX
	} else if strings.HasSuffix(in.DeviceType, "T") {
		maxVoltage = DefaultMaxVoltageNX
		minVoltage = DefaultMinVoltageX
	}
	var remainPower int64
	if int64(in.BatteryVoltage) > maxVoltage {
		remainPower = 100
	} else if int64(in.BatteryVoltage) <= minVoltage {
		remainPower = 1
	} else {
		remainPower = 100 - int64(math.Ceil(float64(maxVoltage-int64(in.BatteryVoltage))/float64(maxVoltage-minVoltage)*10)*10)
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

func FileExist(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}

func GetOurBoundIP() (ip string, err error) {
	// 使用 ipify 的公共 API 来获取公网 IP
	response, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	ipByte, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	return string(ipByte), nil
}
