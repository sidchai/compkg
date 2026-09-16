package impl

import (
	"strings"
	"testing"
)

func TestNormalizeJdcloudPassword(t *testing.T) {
	// 已是 32 位 hex：原样小写
	got := normalizeJdcloudPassword("ABCDEF0123456789ABCDEF0123456789")
	if got != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("md5 hex normalize = %s", got)
	}
	// 明文：再 MD5
	plain := normalizeJdcloudPassword("secret")
	if len(plain) != 32 {
		t.Fatalf("plain md5 len = %d", len(plain))
	}
	if plain != md5HexLower("secret") {
		t.Fatalf("plain md5 mismatch")
	}
}

func TestBuildContentAndSign(t *testing.T) {
	j := &JdcloudSMS{signName: "赛思云"}
	content, err := j.buildContent("您的验证码是{code}，请{minutes}分钟内使用", map[string]string{
		"code":    "123456",
		"minutes": "5",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "【赛思云】您的验证码是123456，请5分钟内使用"
	if content != want {
		t.Fatalf("got %q want %q", content, want)
	}

	// 已含签名不重复
	content2, err := j.buildContent("【赛思云】直接正文", nil)
	if err != nil {
		t.Fatal(err)
	}
	if content2 != "【赛思云】直接正文" {
		t.Fatalf("double sign: %q", content2)
	}

	// content 键优先
	content3, err := j.buildContent("ignored", map[string]string{"content": "自定义正文"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(content3, "【赛思云】") || !strings.Contains(content3, "自定义正文") {
		t.Fatalf("content key: %q", content3)
	}
}

func TestSanitizeAndPwd(t *testing.T) {
	if sanitizeJdcloudContent("a%b&c+d") != "a％b＆c＋d" {
		t.Fatal("sanitize failed")
	}
	mttime := "20250121101010"
	pwd32 := md5HexLower("raw")
	got := md5HexLower(pwd32 + mttime)
	if len(got) != 32 {
		t.Fatalf("pwd len %d", len(got))
	}
}
