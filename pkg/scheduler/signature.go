package scheduler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// sign 生成与服务端 sigverify.Verify 兼容的 HMAC-SHA256 签名。
//
// 算法：signature = hex( HMAC-SHA256(appSecret, appKey + nonce + ts) )
//
// 注意：服务端默认接受小写 hex；本函数始终输出小写。
func sign(appSecret, appKey, nonce string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(appKey))
	mac.Write([]byte(nonce))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// newNonce 生成 16 字符 hex 随机串。crypto/rand 失败时回退到时间戳，保证不阻塞业务。
func newNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// signedCreds 一组签名凭据，供 Connect/SubmitTask 构造请求时复用。
type signedCreds struct {
	Nonce     string
	Ts        int64
	Signature string
}

// newSignedCreds 用当前时间 + 新 nonce 生成一组签名凭据。
func newSignedCreds(appKey, appSecret string) signedCreds {
	nonce := newNonce()
	ts := time.Now().Unix()
	return signedCreds{
		Nonce:     nonce,
		Ts:        ts,
		Signature: sign(appSecret, appKey, nonce, ts),
	}
}
