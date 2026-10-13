package yinbao

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/tjfoc/gmsm/sm3"
	"github.com/tjfoc/gmsm/sm4"
)

// 与客户 hutool 5.8.19 对齐：SmUtil.sm4(key.getBytes()) 是 SM4/ECB/PKCS5Padding，
// encryptHex 输出小写十六进制；签名是 SM3(timestamp + nonce + 明文 + salt) 的小写十六进制。
// 密钥按字符串原始字节使用，不把 hex 再解码成 8 字节。

const sm4KeyLen = 16

func encryptHex(key []byte, plain string) (string, error) {
	if len(key) != sm4KeyLen {
		return "", errors.New("银保 SM4 密钥长度必须是 16 字节")
	}
	out, err := sm4.Sm4Ecb(key, []byte(plain), true)
	if err != nil {
		return "", fmt.Errorf("银保 SM4 加密: %w", err)
	}
	return hex.EncodeToString(out), nil
}

func decryptHex(key []byte, cipherHex string) (string, error) {
	if len(key) != sm4KeyLen {
		return "", errors.New("银保 SM4 密钥长度必须是 16 字节")
	}
	raw := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, cipherHex)
	buf, err := hex.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("银保 SM4 密文不是十六进制: %w", err)
	}
	if len(buf) == 0 || len(buf)%sm4.BlockSize != 0 {
		return "", errors.New("银保 SM4 密文长度不是分组整数倍")
	}
	out, err := sm4.Sm4Ecb(key, buf, false)
	if err != nil {
		return "", fmt.Errorf("银保 SM4 解密: %w", err)
	}
	// tjfoc 在 PKCS7 校验失败时返回 nil, nil，不能当成空明文。
	if out == nil {
		return "", errors.New("银保 SM4 解密填充校验失败")
	}
	return string(out), nil
}

func signSM3(timestamp, nonce, plain, salt string) string {
	sum := sm3.Sm3Sum([]byte(timestamp + nonce + plain + salt))
	return hex.EncodeToString(sum)
}

func newNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("银保生成 nonce: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
