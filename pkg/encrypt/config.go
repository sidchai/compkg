/**
 * @Description: 配置文件敏感信息加解密
 * @Version: 1.0
 * @Author: sidchai
 * @Date: 2026/4/27
 */

package encrypt

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
)

const (
	// EncryptPrefix 加密值前缀标记
	EncryptPrefix = "ENC("
	// EncryptSuffix 加密值后缀标记
	EncryptSuffix = ")"
)

// IsEncrypted 判断值是否为加密格式 ENC(xxx)
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, EncryptPrefix) && strings.HasSuffix(value, EncryptSuffix)
}

// EncryptConfigValue 加密配置值，返回 ENC(Base64密文) 格式
func EncryptConfigValue(key []byte, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	aesGcm := &AesGcm{Key: key}
	encrypted, err := aesGcm.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("加密配置值失败: %w", err)
	}
	return EncryptPrefix + encrypted + EncryptSuffix, nil
}

// DecryptConfigValue 解密配置值，非 ENC() 格式原样返回
func DecryptConfigValue(key []byte, ciphertext string) (string, error) {
	if !IsEncrypted(ciphertext) {
		return ciphertext, nil
	}
	// 提取 ENC(...) 中的密文
	encrypted := ciphertext[len(EncryptPrefix) : len(ciphertext)-len(EncryptSuffix)]
	// Base64 解码
	cryptedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("Base64解码失败: %w", err)
	}
	aesGcm := &AesGcm{Key: key}
	plaintext, err := aesGcm.Decrypt(cryptedBytes)
	if err != nil {
		return "", fmt.Errorf("解密配置值失败: %w", err)
	}
	return plaintext, nil
}

// DecryptMap 遍历 map 所有配置项，解密 ENC() 值（原地修改）
// 用于 viper 场景：传入 viper.AllSettings()，解密后用 viper.Set() 回写
func DecryptMap(key []byte, settings map[string]interface{}) error {
	return decryptMap(key, settings)
}

// decryptMap 递归解密 map 中的所有 ENC() 值
func decryptMap(key []byte, m map[string]interface{}) error {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if IsEncrypted(val) {
				decrypted, err := DecryptConfigValue(key, val)
				if err != nil {
					return fmt.Errorf("解密配置项 %s 失败: %w", k, err)
				}
				m[k] = decrypted
			}
		case map[string]interface{}:
			if err := decryptMap(key, val); err != nil {
				return err
			}
		case []interface{}:
			if err := decryptSlice(key, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// decryptSlice 递归解密 slice 中的所有 ENC() 值
func decryptSlice(key []byte, s []interface{}) error {
	for i, v := range s {
		switch val := v.(type) {
		case string:
			if IsEncrypted(val) {
				decrypted, err := DecryptConfigValue(key, val)
				if err != nil {
					return fmt.Errorf("解密数组元素 [%d] 失败: %w", i, err)
				}
				s[i] = decrypted
			}
		case map[string]interface{}:
			if err := decryptMap(key, val); err != nil {
				return err
			}
		case []interface{}:
			if err := decryptSlice(key, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// DecryptStruct 反射遍历 struct 字段，解密所有 ENC() 字符串值
// 用于 yaml.Unmarshal() 后调用
func DecryptStruct(key []byte, v interface{}) error {
	return decryptValue(key, reflect.ValueOf(v))
}

// decryptValue 递归解密 reflect.Value
func decryptValue(key []byte, v reflect.Value) error {
	// 处理指针
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		return decryptValue(key, v.Elem())
	}

	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.CanSet() {
				continue
			}
			if err := decryptValue(key, field); err != nil {
				return err
			}
		}
	case reflect.String:
		if v.CanSet() {
			str := v.String()
			if IsEncrypted(str) {
				decrypted, err := DecryptConfigValue(key, str)
				if err != nil {
					return err
				}
				v.SetString(decrypted)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := decryptValue(key, v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, mk := range v.MapKeys() {
			mv := v.MapIndex(mk)
			// map 值不能直接修改，需要创建新值
			if mv.Kind() == reflect.String && IsEncrypted(mv.String()) {
				decrypted, err := DecryptConfigValue(key, mv.String())
				if err != nil {
					return err
				}
				v.SetMapIndex(mk, reflect.ValueOf(decrypted))
			} else if mv.Kind() == reflect.Interface {
				// 处理 interface{} 类型的 map 值
				elem := mv.Elem()
				if elem.Kind() == reflect.String && IsEncrypted(elem.String()) {
					decrypted, err := DecryptConfigValue(key, elem.String())
					if err != nil {
						return err
					}
					v.SetMapIndex(mk, reflect.ValueOf(decrypted))
				}
			}
		}
	}
	return nil
}

// ParseEncryptKey 解析 Base64 编码的密钥
func ParseEncryptKey(keyStr string) ([]byte, error) {
	if keyStr == "" {
		return nil, fmt.Errorf("加密密钥不能为空")
	}
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		return nil, fmt.Errorf("密钥Base64解码失败: %w", err)
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("密钥长度必须为16字节(AES-128)，当前: %d", len(key))
	}
	return key, nil
}
