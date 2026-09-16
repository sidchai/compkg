package impl

import "testing"

func TestUnescapeOSSObjectKey(t *testing.T) {
	// 单次编码 → 中文
	got := unescapeOSSObjectKey("excel/2026/08/12/8_%E5%BD%95%E9%9F%B3%E8%AE%B0%E5%BD%95.xlsx")
	want := "excel/2026/08/12/8_录音记录.xlsx"
	if got != want {
		t.Fatalf("single encode: got %q want %q", got, want)
	}
	// 双重编码 → 中文
	got = unescapeOSSObjectKey("excel/2026/08/12/8_%25E5%25BD%2595%25E9%259F%25B3%25E8%25AE%25B0%25E5%25BD%2595.xlsx")
	if got != want {
		t.Fatalf("double encode: got %q want %q", got, want)
	}
	// 已是中文
	got = unescapeOSSObjectKey(want)
	if got != want {
		t.Fatalf("plain utf8: got %q want %q", got, want)
	}
}

func TestObjectKeyFromURL(t *testing.T) {
	j := &JDCloudOss{
		bucketName: "saisiyun",
		endpoint:   "s3.cn-north-1.jdcloud-oss.com",
	}
	// 公网 + 单次编码
	url1 := "https://saisiyun.s3.cn-north-1.jdcloud-oss.com/excel/2026/08/12/8_20260812_110725-%E5%BD%95%E9%9F%B3%E8%AE%B0%E5%BD%95.xlsx"
	got := j.objectKeyFromURL(url1)
	want := "excel/2026/08/12/8_20260812_110725-录音记录.xlsx"
	if got != want {
		t.Fatalf("public encoded: got %q want %q", got, want)
	}
	// 内网 + 双重编码
	url2 := "https://saisiyun.s3-internal.cn-north-1.jdcloud-oss.com/excel/2026/08/12/8_20260812_110725-%25E5%25BD%2595%25E9%259F%25B3%25E8%25AE%25B0%25E5%25BD%2595.xlsx"
	got = j.objectKeyFromURL(url2)
	if got != want {
		t.Fatalf("internal double encoded: got %q want %q", got, want)
	}
	// 带 query 的预签名再解析
	url3 := url1 + "?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc"
	got = j.objectKeyFromURL(url3)
	if got != want {
		t.Fatalf("with query: got %q want %q", got, want)
	}
}
