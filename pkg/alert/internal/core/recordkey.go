package core

import "strings"

// RecordKey 统一录音阶段 bizKey：只留文件名，去掉扩展名。
// Inform / MQ / 点查必须用同一把钥匙，避免 .RAW 或路径导致起止对不上而误告。
func RecordKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, `\`, `/`)
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	for _, ext := range []string{".RAW", ".raw", ".MP3", ".mp3", ".WAV", ".wav", ".TXT", ".txt"} {
		raw = strings.TrimSuffix(raw, ext)
	}
	return raw
}

// RecordKeyVariants 点查时兼容库里带或不带后缀的历史数据。
func RecordKeyVariants(raw string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(raw)
	k := RecordKey(raw)
	add(k)
	if k != "" {
		add(k + ".RAW")
		add(k + ".raw")
	}
	return out
}
