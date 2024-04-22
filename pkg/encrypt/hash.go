package encrypt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

type Sha256 struct {
	Key string
}

func NewSha256(key string) *Sha256 {
	return &Sha256{
		Key: key,
	}
}

// 加密
func (s *Sha256) encrypt(message string) []byte {
	key := []byte(s.Key)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))

	return h.Sum(nil)
}

// ToHex 将加密后的二进制转16进制字符串
func (s *Sha256) ToHex(message string) string {
	return hex.EncodeToString(s.encrypt(message))
}

// ToBase64 将加密后的二进制转base64
func (s *Sha256) ToBase64(message string) string {
	return base64.StdEncoding.EncodeToString(s.encrypt(message))
}
