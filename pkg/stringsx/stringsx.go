/**
 * @Description: 字符串工具类
 * @Version: 1.0
 * @Author: sidchai
 * @Date: 2022/2/14 16:36
 */

package stringsx

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// InStringArray
//
//	@Author: sidchai
//	@Description: 判断字符串是否在字符串数组中
//	@param target 目标字符串
//	@param strArray 目标字符串数组
//	@return bool 结果
func InStringArray(target string, strArray []string) bool {
	// 对字符串切片进行排序
	sort.Strings(strArray)
	index := sort.SearchStrings(strArray, target)
	// 先判断 &&左侧的条件，如果不满足则结束此处判断，不会再进行右侧的判断
	if index < len(strArray) && strArray[index] == target {
		return true
	}
	return false
}

func IntIntoString(data int) string {
	return strconv.Itoa(data)
}

func StringIntoInt(str string) int {
	atoi, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return atoi
}

func StringIntoInt64(str string) int64 {
	i, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return 0
	}
	return i
}

func StringToFloat64(str string) float64 {
	parseFloat, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0
	}
	return parseFloat
}

func Int64IntoString(data int64) string {
	formatInt := strconv.FormatInt(data, 10)
	return formatInt
}

func InterfaceIntoString(data interface{}) string {
	if data == nil {
		return ""
	}
	return string(data.([]byte))
}

// RemoveQuotes
//
//	@Description: 删除字符串中双引号
func RemoveQuotes(input string) string {
	return strings.ReplaceAll(input, "\"", "")
}

// IsUpperCase 如果在A-Z中的符文返回true
func IsUpperCase(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	return false
}

// IsLowerCase 如果a-z中的符文返回true
func IsLowerCase(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	return false
}

// ToSnakeCase
//
//	@Description: 通过将驼峰格式转换为蛇格式返回一个复制字符串
//	@param s 驼峰格式字符串
//	@return string 下划线字符串
func ToSnakeCase(s string) string {
	var out []rune
	for index, r := range s {
		if index == 0 {
			out = append(out, ToLowerCase(r))
			continue
		}

		if IsUpperCase(r) && index != 0 {
			if IsLowerCase(rune(s[index-1])) {
				out = append(out, '_', ToLowerCase(r))
				continue
			}
			if index < len(s)-1 && IsLowerCase(rune(s[index+1])) {
				out = append(out, '_', ToLowerCase(r))
				continue
			}
			out = append(out, ToLowerCase(r))
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// ToCamelCase
//
//	@Description: 通过将蛇形大小写转换为驼峰大小写返回一个复制字符串
//	@param s
//	@return string
func ToCamelCase(s string) string {
	s = ToLower(s)
	out := []rune{}
	for index, r := range s {
		if r == '_' {
			continue
		}
		if index == 0 {
			out = append(out, ToUpperCase(r))
			continue
		}

		if index > 0 && s[index-1] == '_' {
			out = append(out, ToUpperCase(r))
			continue
		}

		out = append(out, r)
	}
	return string(out)
}

// ToLowerCase 将符文转换为小写
func ToLowerCase(r rune) rune {
	dx := 'A' - 'a'
	if IsUpperCase(r) {
		return r - dx
	}
	return r
}

// ToUpperCase 将符文转换为大写
func ToUpperCase(r rune) rune {
	dx := 'A' - 'a'
	if IsLowerCase(r) {
		return r + dx
	}
	return r
}

// ToLower 将字符串转为小写
func ToLower(s string) string {
	var out []rune
	for _, r := range s {
		out = append(out, ToLowerCase(r))
	}
	return string(out)
}

// ToUpper 将小写字母转为大写返回一个复制字符串
func ToUpper(s string) string {
	var out []rune
	for _, r := range s {
		out = append(out, ToUpperCase(r))
	}
	return string(out)
}

// UpperFirst 将第一个字母转换为大写
func UpperFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return ToUpper(s[:1]) + s[1:]
}

// UnExport 将第一个字母转换为小写
func UnExport(text string) string {
	var flag bool
	str := strings.Map(func(r rune) rune {
		if flag {
			return r
		}
		if unicode.IsLetter(r) {
			flag = true
			return unicode.ToLower(r)
		}
		return r
	}, text)
	return str
}
