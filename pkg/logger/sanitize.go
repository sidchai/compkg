package logger

import (
	"regexp"
	"strings"
)

// SanitizeRule 单条脱敏规则。
//
// 命中策略：
//   - FieldName 非空 → 字段名精确匹配（用于 With(key, value) 写入的 key）
//   - Pattern 非 nil → 对 message 文本和字符串 value 做正则替换
//
// Replace 必填，接收原值返回脱敏后的值。
type SanitizeRule struct {
	FieldName string
	Pattern   *regexp.Regexp
	Replace   func(string) string
}

// DisableSanitize 显式禁用脱敏（在 BootstrapOptions.SanitizeRules 中传入）。
var DisableSanitize = []SanitizeRule{}

// 内置常见 PII / 密钥脱敏规则。
var defaultSanitizeRules = []SanitizeRule{
	// 字段名命中：直接用 *** 替换值
	{FieldName: "password", Replace: maskAll},
	{FieldName: "passwd", Replace: maskAll},
	{FieldName: "pwd", Replace: maskAll},
	{FieldName: "access_token", Replace: maskAll},
	{FieldName: "refresh_token", Replace: maskAll},
	{FieldName: "secret", Replace: maskAll},
	{FieldName: "api_key", Replace: maskAll},
	{FieldName: "apikey", Replace: maskAll},
	{FieldName: "callback_sign_key", Replace: maskAll},
	{FieldName: "encrypt_key", Replace: maskAll},
	{FieldName: "phone", Replace: maskPhone},
	{FieldName: "mobile", Replace: maskPhone},
	{FieldName: "id_card", Replace: maskIdCard},
	{FieldName: "idcard", Replace: maskIdCard},

	// 正则命中：消息体里裸出的手机号 / 身份证
	{Pattern: regexp.MustCompile(`1[3-9]\d{9}`), Replace: maskPhone},
	{Pattern: regexp.MustCompile(`\d{17}[\dXx]`), Replace: maskIdCard},
}

// DefaultSanitizeRules 返回内置规则的拷贝（避免业务方修改污染全局）。
func DefaultSanitizeRules() []SanitizeRule {
	out := make([]SanitizeRule, len(defaultSanitizeRules))
	copy(out, defaultSanitizeRules)
	return out
}

func maskAll(string) string { return "***" }

// maskPhone 138****8888 形式。短于 7 位则全脱敏。
func maskPhone(s string) string {
	if len(s) < 7 {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + "****" + s[len(s)-4:]
}

// maskIdCard 保留前 3 位和后 4 位，中间 *。
func maskIdCard(s string) string {
	if len(s) < 7 {
		return strings.Repeat("*", len(s))
	}
	return s[:3] + strings.Repeat("*", len(s)-7) + s[len(s)-4:]
}

// applySanitize 对 (key, value) 做脱敏：
//   - value 是 string 时，先按字段名命中、再对值做正则；
//   - 非 string 类型按字段名命中转为 "***"（如 token 数字误传）；
//   - 找不到任何规则则原样返回。
func applySanitize(rules []SanitizeRule, key string, value any) any {
	if len(rules) == 0 {
		return value
	}
	keyLower := strings.ToLower(key)
	for _, r := range rules {
		if r.FieldName != "" && strings.EqualFold(keyLower, r.FieldName) {
			if s, ok := value.(string); ok {
				return r.Replace(s)
			}
			return "***"
		}
	}
	if s, ok := value.(string); ok {
		for _, r := range rules {
			if r.Pattern != nil {
				s = r.Pattern.ReplaceAllStringFunc(s, r.Replace)
			}
		}
		return s
	}
	return value
}

// applyMessageSanitize 对消息文本做正则脱敏。
func applyMessageSanitize(rules []SanitizeRule, msg string) string {
	if len(rules) == 0 || msg == "" {
		return msg
	}
	for _, r := range rules {
		if r.Pattern != nil {
			msg = r.Pattern.ReplaceAllStringFunc(msg, r.Replace)
		}
	}
	return msg
}
