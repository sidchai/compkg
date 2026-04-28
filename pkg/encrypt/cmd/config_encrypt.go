/**
 * @Description: 配置加密CLI工具
 * @Version: 1.1
 * @Author: sidchai
 * @Date: 2026/4/27
 *
 * 使用方式:
 *   go run config_encrypt.go encrypt --key="Base64密钥" "值1" "值2" "值3"
 *   go run config_encrypt.go decrypt --key="Base64密钥" "ENC(密文1)" "ENC(密文2)"
 *
 * 或编译后使用:
 *   go build -o config-encrypt config_encrypt.go
 *   ./config-encrypt encrypt --key="Base64密钥" "值1" "值2" "值3"
 */

package main

import (
	"fmt"
	"os"

	"github.com/sidchai/compkg/pkg/encrypt"
)

func main() {
	if len(os.Args) < 4 {
		printUsage()
		os.Exit(1)
	}

	action := os.Args[1]
	keyFlag := os.Args[2]
	values := os.Args[3:]

	// 解析 --key=xxx 参数
	if len(keyFlag) < 7 || keyFlag[:6] != "--key=" {
		fmt.Println("错误: 缺少 --key 参数")
		printUsage()
		os.Exit(1)
	}
	keyStr := keyFlag[6:]

	key, err := encrypt.ParseEncryptKey(keyStr)
	if err != nil {
		fmt.Printf("错误: %v\n", err)
		os.Exit(1)
	}

	switch action {
	case "encrypt":
		for _, value := range values {
			result, err := encrypt.EncryptConfigValue(key, value)
			if err != nil {
				fmt.Printf("加密失败 [%s]: %v\n", value, err)
				os.Exit(1)
			}
			fmt.Printf("%s: %s\n", value, result)
		}

	case "decrypt":
		for _, value := range values {
			result, err := encrypt.DecryptConfigValue(key, value)
			if err != nil {
				fmt.Printf("解密失败 [%s]: %v\n", value, err)
				os.Exit(1)
			}
			fmt.Printf("%s: %s\n", value, result)
		}

	default:
		fmt.Printf("未知操作: %s\n", action)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`配置加密工具

用法:
  config-encrypt encrypt --key="Base64密钥" "值1" "值2" ...
  config-encrypt decrypt --key="Base64密钥" "ENC(密文1)" "ENC(密文2)" ...

生成密钥:
  openssl rand -base64 16

示例:
  # 批量加密
  config-encrypt encrypt --key="K7gNU3sdo+OL0wNh" "127.0.0.1:3306" "root" "password123"
  # 输出:
  # 127.0.0.1:3306: ENC(xxx)
  # root: ENC(xxx)
  # password123: ENC(xxx)

  # 批量解密
  config-encrypt decrypt --key="K7gNU3sdo+OL0wNh" "ENC(xxx)" "ENC(yyy)"
  # 输出:
  # ENC(xxx): 127.0.0.1:3306
  # ENC(yyy): root`)
}
