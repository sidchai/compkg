package encrypt

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"github.com/sidchai/compkg/pkg/stringsx"
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

// ToStdBase64 将加密后的二进制转标准base64
func (s *Sha256) ToStdBase64(message string) string {
	return base64.StdEncoding.EncodeToString(s.encrypt(message))
}

// ToUrlBase64 将加密后的二进制转Url base64
func (s *Sha256) ToUrlBase64(message string) string {
	return base64.URLEncoding.EncodeToString(s.encrypt(message))
}

type Sha1 struct {
	Key string
}

func NewSha1(key string) *Sha1 {
	return &Sha1{
		Key: key,
	}
}

// 加密
func (s *Sha1) encrypt(message string) []byte {
	key := []byte(s.Key)
	h := hmac.New(sha1.New, key)
	h.Write([]byte(message))

	return h.Sum(nil)
}

// ToHex 将加密后的二进制转16进制字符串
func (s *Sha1) ToHex(message string) string {
	return hex.EncodeToString(s.encrypt(message))
}

// ToStdBase64 将加密后的二进制转标准base64
func (s *Sha1) ToStdBase64(message string) string {
	return base64.StdEncoding.EncodeToString([]byte(stringsx.ToUpper(message)))
}

// ToUrlBase64 将加密后的二进制转Url base64
func (s *Sha1) ToUrlBase64(message string) string {
	return base64.URLEncoding.EncodeToString(s.encrypt(message))
}
