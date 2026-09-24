package miniox

import (
	"testing"
	"time"
)

func TestObjectNameFromURL(t *testing.T) {
	m := &MinioX{BucketName: "mybucket"}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"空串", "", ""},
		{"裸 key", "audio/2026/09/21/a.mp3", "audio/2026/09/21/a.mp3"},
		{"裸 key 前导斜杠", "/audio/a.mp3", "audio/a.mp3"},
		{"path-style 完整 URL", "http://127.0.0.1:9000/mybucket/audio/2026/09/21/a.mp3", "audio/2026/09/21/a.mp3"},
		{"去掉已有签名 query", "http://127.0.0.1:9000/mybucket/audio/a.mp3?X-Amz-Signature=abc", "audio/a.mp3"},
		{"虚拟主机风格", "https://mybucket.s3.amazonaws.com/audio/a.mp3", "audio/a.mp3"},
		{"中文 key 一次编码", "http://127.0.0.1:9000/mybucket/audio/%E5%BD%95%E9%9F%B3.mp3", "audio/录音.mp3"},
		{"中文 key 二次编码", "http://127.0.0.1:9000/mybucket/audio/%25E5%25BD%2595.mp3", "audio/录.mp3"},
		{"https path-style", "https://minio.example.com/mybucket/dir/file.bin", "dir/file.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.objectNameFromURL(tt.input)
			if got != tt.want {
				t.Errorf("objectNameFromURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPresignExpires_ZeroDefaultsTo3600(t *testing.T) {
	m := &MinioX{}
	if got := m.presignExpires(); got != 3600*time.Second {
		t.Errorf("Expires=0 应得 3600s, got %s", got)
	}
	m.Expires = 120
	if got := m.presignExpires(); got != 120*time.Second {
		t.Errorf("Expires=120 应得 120s, got %s", got)
	}
}

func TestGetPresignedURL_NilClient(t *testing.T) {
	m := &MinioX{BucketName: "mybucket"}
	_, err := m.GetPresignedURL("http://127.0.0.1:9000/mybucket/a.mp3")
	if err == nil {
		t.Fatal("client 为 nil 时应返回 error")
	}
}

func TestGetPresignedURL_EmptyKey(t *testing.T) {
	m := &MinioX{BucketName: "mybucket", client: nil}
	// client 先判 nil，空 key 走不到；单独测解析
	if got := m.objectNameFromURL("   "); got != "" {
		t.Errorf("空白 URL 应得空 key, got %q", got)
	}
}
