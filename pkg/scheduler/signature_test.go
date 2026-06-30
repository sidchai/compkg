package scheduler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

// 与 iot_scheduler/internal/sigverify/hmac.go 的算法严格对齐：
//
//	signature = hex( HMAC-SHA256(appSecret, appKey + nonce + ts) )
func TestSign_MatchesServerAlgo(t *testing.T) {
	const (
		appSecret = "s3cret-987"
		appKey    = "ak_abc123"
		nonce     = "deadbeef"
	)
	ts := int64(1700000000)

	got := sign(appSecret, appKey, nonce, ts)

	// 本地手算期望值
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(appKey))
	mac.Write([]byte(nonce))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("sign mismatch:\n got=%s\nwant=%s", got, want)
	}
}

func TestNewNonce_HexNonEmpty(t *testing.T) {
	n1 := newNonce()
	n2 := newNonce()
	if len(n1) == 0 || len(n2) == 0 {
		t.Fatalf("nonce empty: %q %q", n1, n2)
	}
	if n1 == n2 {
		t.Fatalf("expect different nonces, got identical: %s", n1)
	}
	if _, err := hex.DecodeString(n1); err != nil {
		t.Fatalf("nonce not hex: %v", err)
	}
}

func TestNewSignedCreds_Verifiable(t *testing.T) {
	const (
		appKey    = "ak_test"
		appSecret = "ss_test"
	)
	creds := newSignedCreds(appKey, appSecret)

	if creds.Nonce == "" || creds.Signature == "" {
		t.Fatalf("creds incomplete: %+v", creds)
	}
	// ts 必须落在当前时间附近 5min 内（防重放窗口）
	now := time.Now().Unix()
	if diff := now - creds.Ts; diff < 0 || diff > 5 {
		t.Fatalf("ts skew too large: now=%d ts=%d", now, creds.Ts)
	}

	// 本地复算并比较
	expected := sign(appSecret, appKey, creds.Nonce, creds.Ts)
	if creds.Signature != expected {
		t.Fatalf("creds.Signature mismatch:\n got=%s\nwant=%s", creds.Signature, expected)
	}
}
