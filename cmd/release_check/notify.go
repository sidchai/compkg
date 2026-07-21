package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// notifyResult 汇总各通知渠道的发送结果，供日志输出。
type notifyResult struct {
	channel string
	err     error
}

// dispatchNotify 按配置触发各通知渠道。allPass 用于"仅失败时通知"判定。
// 返回各渠道结果（含跳过的不计入），调用方负责打印。
func dispatchNotify(cfg *CheckConfig, title, markdown string, allPass bool) []notifyResult {
	var results []notifyResult

	if cfg.Notify.DingTalk.Enabled {
		if cfg.Notify.DingTalk.OnlyOnFailure && allPass {
			// 配置为仅失败通知且本次通过 → 跳过
		} else {
			err := sendDingTalk(cfg.Notify.DingTalk, title, markdown)
			results = append(results, notifyResult{channel: "钉钉", err: err})
		}
	}

	if cfg.Notify.Email.Enabled {
		if cfg.Notify.Email.OnlyOnFailure && allPass {
			// 跳过
		} else {
			err := sendEmail(cfg.Notify.Email, title, markdown)
			results = append(results, notifyResult{channel: "邮件", err: err})
		}
	}

	return results
}

// sendDingTalk 发送钉钉 markdown 消息（支持加签）。
func sendDingTalk(conf DingTalkConf, title, markdown string) error {
	webhookURL := conf.Webhook
	if conf.Secret != "" {
		webhookURL = signDingURL(webhookURL, conf.Secret)
	}

	body := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  markdown,
		},
	}
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("钉钉消息序列化失败: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("钉钉请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("钉钉响应状态 %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.ErrCode != 0 {
		return fmt.Errorf("钉钉返回错误 code=%d msg=%s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

// signDingURL 钉钉加签：HMAC-SHA256(timestamp\nsecret)。
func signDingURL(webhookURL, secret string) string {
	timestamp := time.Now().UnixMilli()
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(h.Sum(nil)))
	return fmt.Sprintf("%s&timestamp=%d&sign=%s", webhookURL, timestamp, sign)
}

// sendEmail 通过 SMTP 发送报告邮件。
// 465 端口走 SSL（隐式 TLS），其它端口走明文/STARTTLS（多数企业邮箱用 465）。
func sendEmail(conf EmailConf, subject, markdown string) error {
	addr := fmt.Sprintf("%s:%d", conf.SmtpHost, conf.SmtpPort)
	auth := smtp.PlainAuth("", conf.Username, conf.Password, conf.SmtpHost)

	// 从 From 字段解析裸邮箱地址，用于 SMTP 信封（MAIL FROM 命令）。
	// conf.From 可能是 "显示名<email>" 格式，SMTP 命令只接受裸地址。
	envFrom := conf.From
	if parsed, err := mail.ParseAddress(conf.From); err == nil {
		envFrom = parsed.Address
	}

	// 邮件正文用纯文本承载 markdown（多数客户端可读；避免引入 HTML 转换依赖）。
	msg := buildEmailMessage(conf.From, conf.To, subject, markdown)

	if conf.SmtpPort == 465 {
		return sendMailSSL(addr, conf.SmtpHost, auth, envFrom, conf.To, msg)
	}
	// 587/25：标准库 smtp.SendMail 内部会尝试 STARTTLS
	return smtp.SendMail(addr, auth, envFrom, conf.To, msg)
}

// buildEmailMessage 构造 RFC822 邮件报文（含中文主题 Base64 编码）。
// body 为 Markdown 格式，内部转换为 HTML 发送，邮件客户端可正确渲染。
func buildEmailMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	b.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ",")))
	// 主题用 RFC2047 Base64 编码，避免中文乱码
	b.WriteString(fmt.Sprintf("Subject: =?UTF-8?B?%s?=\r\n", base64.StdEncoding.EncodeToString([]byte(subject))))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(markdownToHTML(body))
	return []byte(b.String())
}

// ──────────────────────────────────────────────────
// 轻量 Markdown → HTML 转换（仅覆盖报告使用的子集）
// 支持：# 标题、- 列表、> 引用、| 表格、**粗体**、`代码`、_斜体_
// ──────────────────────────────────────────────────

// markdownToHTML 将报告 Markdown 转换为带内联样式的 HTML 邮件正文。
func markdownToHTML(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var b strings.Builder

	// 邮件客户端不支持外部 CSS，使用内联样式
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head>`)
	b.WriteString(`<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#333;line-height:1.6;padding:20px;max-width:900px;margin:0 auto;">`)

	inList := false  // 当前是否在 <ul> 块中
	inTable := false // 当前是否在 <table> 块中

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// 退出列表块
		if inList && !strings.HasPrefix(trimmed, "- ") {
			b.WriteString("</ul>\n")
			inList = false
		}
		// 退出表格块
		if inTable && !strings.HasPrefix(trimmed, "|") {
			b.WriteString("</tbody></table>\n")
			inTable = false
		}

		// 空行跳过
		if trimmed == "" {
			continue
		}

		// ### 三级标题
		if strings.HasPrefix(trimmed, "### ") {
			b.WriteString(fmt.Sprintf(`<h3 style="color:#2c2c2c;margin:16px 0 8px;">%s</h3>`+"\n", mdInline(trimmed[4:])))
			continue
		}
		// ## 二级标题
		if strings.HasPrefix(trimmed, "## ") {
			b.WriteString(fmt.Sprintf(`<h2 style="color:#2c2c2c;border-bottom:1px solid #eee;padding-bottom:6px;margin:20px 0 10px;">%s</h2>`+"\n", mdInline(trimmed[3:])))
			continue
		}
		// # 一级标题
		if strings.HasPrefix(trimmed, "# ") {
			b.WriteString(fmt.Sprintf(`<h1 style="color:#1a1a1a;border-bottom:2px solid #e0e0e0;padding-bottom:8px;margin:0 0 16px;">%s</h1>`+"\n", mdInline(trimmed[2:])))
			continue
		}

		// > 引用块
		if strings.HasPrefix(trimmed, "> ") {
			b.WriteString(fmt.Sprintf(`<blockquote style="border-left:4px solid #4CAF50;margin:12px 0;padding:8px 16px;background:#f9f9f9;color:#555;">%s</blockquote>`+"\n", mdInline(trimmed[2:])))
			continue
		}

		// - 无序列表
		if strings.HasPrefix(trimmed, "- ") {
			if !inList {
				b.WriteString(`<ul style="padding-left:24px;margin:8px 0;">` + "\n")
				inList = true
			}
			b.WriteString(fmt.Sprintf(`<li style="margin:4px 0;">%s</li>`+"\n", mdInline(trimmed[2:])))
			continue
		}

		// | 表格行
		if strings.HasPrefix(trimmed, "|") {
			// 跳过分隔行 |---|---|
			if mdIsTableSep(trimmed) {
				continue
			}
			cells := mdParseTableRow(trimmed)
			if !inTable {
				// 首行作为表头
				b.WriteString(`<table style="border-collapse:collapse;width:100%;margin:12px 0;font-size:14px;">` + "\n")
				b.WriteString("<thead><tr>")
				for _, c := range cells {
					b.WriteString(fmt.Sprintf(`<th style="border:1px solid #ddd;padding:8px 12px;text-align:left;background:#f5f5f5;font-weight:600;">%s</th>`, mdInline(strings.TrimSpace(c))))
				}
				b.WriteString("</tr></thead>\n<tbody>\n")
				inTable = true
				continue
			}
			b.WriteString("<tr>")
			for _, c := range cells {
				b.WriteString(fmt.Sprintf(`<td style="border:1px solid #ddd;padding:8px 12px;text-align:left;">%s</td>`, mdInline(strings.TrimSpace(c))))
			}
			b.WriteString("</tr>\n")
			continue
		}

		// --- 水平线
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			b.WriteString(`<hr style="border:none;border-top:1px solid #eee;margin:16px 0;">` + "\n")
			continue
		}

		// 普通段落
		b.WriteString(fmt.Sprintf("<p style=\"margin:8px 0;\">%s</p>\n", mdInline(trimmed)))
	}

	// 关闭未结束的块
	if inList {
		b.WriteString("</ul>\n")
	}
	if inTable {
		b.WriteString("</tbody></table>\n")
	}
	b.WriteString("</body></html>")
	return b.String()
}

// mdInline 处理行内 Markdown 格式：**粗体**、`行内代码`、_斜体_。
func mdInline(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		// **粗体**
		if i+1 < len(s) && s[i] == '*' && s[i+1] == '*' {
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				out.WriteString("<strong>")
				out.WriteString(mdInline(s[i+2 : i+2+end]))
				out.WriteString("</strong>")
				i += 2 + end + 2
				continue
			}
		}
		// `行内代码`
		if s[i] == '`' {
			if end := strings.Index(s[i+1:], "`"); end >= 0 {
				out.WriteString(fmt.Sprintf(`<code style="background:#f0f0f0;padding:2px 6px;border-radius:3px;font-size:0.9em;">%s</code>`, s[i+1:i+1+end]))
				i += 1 + end + 1
				continue
			}
		}
		// _斜体_（确保不在单词中间触发，要求前后为非字母）
		if s[i] == '_' && (i == 0 || s[i-1] == ' ') {
			if end := strings.Index(s[i+1:], "_"); end >= 0 {
				endPos := i + 1 + end
				if endPos+1 >= len(s) || s[endPos+1] == ' ' {
					out.WriteString("<em>")
					out.WriteString(mdInline(s[i+1 : endPos]))
					out.WriteString("</em>")
					i = endPos + 1
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// mdIsTableSep 判断是否为表格分隔行，如 |---|---|---| 。
func mdIsTableSep(line string) bool {
	cleaned := strings.NewReplacer(" ", "", "-", "", "|", "", ":", "").Replace(line)
	return cleaned == ""
}

// mdParseTableRow 解析表格行为单元格切片，去除首尾 | 后按 | 分割。
func mdParseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return strings.Split(line, "|")
}

// sendMailSSL 走隐式 TLS（465 端口）发送邮件。标准库 smtp.SendMail 不支持隐式 TLS，需手动建连。
func sendMailSSL(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("SMTP SSL 连接失败: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer client.Quit()

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL 命令失败: %w", err)
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("SMTP RCPT %s 失败: %w", addr, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA 命令失败: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("写入邮件正文失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("关闭邮件写入失败: %w", err)
	}
	return nil
}
