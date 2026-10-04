package core

import (
	"fmt"
	"net"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Issue 描述 hosts 内容的一处问题。
// Warn=false 是错误（阻止写入），Warn=true 是警告（允许写入但提醒用户）。
type Issue struct {
	Line int    // 行号（1 起）；0 表示整文件级问题
	Msg  string // 中文描述
	Warn bool
}

// HasError 报告 issues 里是否包含错误级问题。
func HasError(issues []Issue) bool {
	for _, it := range issues {
		if !it.Warn {
			return true
		}
	}
	return false
}

// ValidateHosts 对 hosts 文本做语法校验。
// 规则：
//   - 空行与 # 开头注释行跳过；行内 # 之后视为注释
//   - 每行格式：IP + 至少一个域名
//   - IP 必须是合法 IPv4/IPv6（net.ParseIP）
//   - 域名只允许字母数字点横杠；下划线给警告（非标准，部分系统不认）
//   - 控制字符、非法字符直接报错
//   - 同一域名指向不同 IP → 警告（可能是手误）
//   - 完全重复的行 → 警告
func ValidateHosts(content string) []Issue {
	var issues []Issue
	// 统一换行符：Windows 的 CRLF 也照单全收
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")

	seenHost := map[string]int{}      // 域名 -> 首次出现的行号
	seenHostIP := map[string]string{} // 域名 -> 首次指向的 IP
	seenLine := map[string]int{}      // 规范化后的整行 -> 行号

	for i, raw := range lines {
		no := i + 1
		// 非法字符前置检查：控制字符（除了 \t）
		for _, r := range raw {
			if r == '\t' {
				continue
			}
			if unicode.IsControl(r) {
				issues = append(issues, Issue{Line: no, Msg: "含有不可见的控制字符，可能是从网页复制时带入的"})
				break
			}
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 切掉行内注释
		code := trimmed
		if idx := strings.Index(code, "#"); idx >= 0 {
			code = strings.TrimSpace(code[:idx])
		}
		if code == "" {
			continue
		}
		fields := strings.Fields(code)
		if len(fields) < 2 {
			issues = append(issues, Issue{Line: no, Msg: fmt.Sprintf("格式不对：「%s」——每行至少是「IP 域名」，例如 127.0.0.1 mydev.local", truncate(raw, 60))})
			continue
		}
		ip := fields[0]
		if net.ParseIP(ip) == nil {
			issues = append(issues, Issue{Line: no, Msg: fmt.Sprintf("IP 地址格式不正确：「%s」", truncate(ip, 40))})
			continue
		}
		for _, h := range fields[1:] {
			lh := strings.ToLower(h)
			if bad, why := badHostname(h); bad {
				// 兼容性策略（评审第 12 条）：真正无法用的判错误并阻止写入；
				// 非标准但可能有效的降级为警告，不阻止。
				// 横杠开头/结尾属于后者（部分内网 DNS 照单全收）。
				if why == "hyphen-edge" {
					issues = append(issues, Issue{Line: no, Warn: true,
						Msg: fmt.Sprintf("域名「%s」以横杠开头或结尾：非标准写法，部分系统可能不识别", truncate(h, 40))})
				} else {
					issues = append(issues, Issue{Line: no, Msg: fmt.Sprintf("域名不合法「%s」：%s", truncate(h, 40), why)})
					continue
				}
			}
			if strings.Contains(h, "_") {
				issues = append(issues, Issue{Line: no, Warn: true, Msg: fmt.Sprintf("域名「%s」含下划线：不是标准域名，部分系统可能不识别", truncate(h, 40))})
			}
			if prevLine, ok := seenHost[lh]; ok {
				if prevIP := seenHostIP[lh]; prevIP != ip {
					issues = append(issues, Issue{Line: no, Warn: true,
						Msg: fmt.Sprintf("域名「%s」在第 %d 行指向 %s，这里又指向 %s：以后者为准，确认不是手误", h, prevLine, prevIP, ip)})
				}
			} else {
				seenHost[lh] = no
				seenHostIP[lh] = ip
			}
		}
		// 整行重复检测（规范化后比较）
		norm := strings.Join(fields, " ")
		if prevLine, ok := seenLine[norm]; ok {
			issues = append(issues, Issue{Line: no, Warn: true,
				Msg: fmt.Sprintf("与第 %d 行完全重复，多写一行不影响生效，但建议删掉", prevLine)})
		} else {
			seenLine[norm] = no
		}
	}
	return issues
}

// badHostname 检查域名是否合法，返回 (是否非法, 原因)。
func badHostname(h string) (bool, string) {
	if len(h) == 0 || len(h) > 253 {
		return true, "长度不对（1~253 个字符）"
	}
	// 允许末尾一个点（FQDN 写法），先去掉再判
	name := strings.TrimSuffix(h, ".")
	if name == "" {
		return true, "不能为空"
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return true, "有一节为空或超过 63 个字符"
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return true, "hyphen-edge" // 非标准但部分系统可用，调用方降级为警告
		}
		for _, r := range label {
			if r == '_' {
				continue // 下划线另作警告处理
			}
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return true, fmt.Sprintf("含有非法字符「%c」，只允许字母、数字、点、横杠", r)
			}
		}
	}
	return false, ""
}

// NormalizeHosts 把内容规范化：统一换行、去掉行尾空格、保证末尾换行。
func NormalizeHosts(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// CountLines 返回文本行数（空文本计 0 行），用于恢复/撤销前的行数对比展示。
func CountLines(content string) int {
	if strings.TrimSpace(content) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), "\n"), "\n"))
}

// CountStats 返回行数/有效条目数，给 UI 展示用。
func CountStats(content string) (total, active int) {
	for _, l := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		total++
		if !strings.HasPrefix(t, "#") {
			active++
		}
	}
	return total, active
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
