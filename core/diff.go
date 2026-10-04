package core

import (
	"fmt"
	"strings"
)

// DiffOp 是 diff 行的操作类型：' ' 不变，'+' 新增，'-' 删除。
type DiffLine struct {
	Op   byte   // ' ', '+', '-'
	Text string // 行内容（不含换行符）
}

// DiffHosts 对两个 hosts 文本做行级 diff（LCS 算法）。
// hosts 文件通常只有几百行，O(n*m) 足够快。
func DiffHosts(oldText, newText string) []DiffLine {
	a := splitLines(oldText)
	b := splitLines(newText)
	// LCS DP（用行哈希压缩内存也行，这里直接存 int 表，hosts 规模下没问题）
	n, m := len(a), len(b)
	// 为防超大文件，做个上限保护
	if n*m > 4_000_000 {
		return []DiffLine{{Op: ' ', Text: fmt.Sprintf("（文件太大，跳过 diff：%d 行 vs %d 行）", n, m)}}
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, DiffLine{'-', a[i]})
			i++
		default:
			out = append(out, DiffLine{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{'+', b[j]})
	}
	return out
}

// DiffSummary 统计 diff 的增删行数，给确认框用。
func DiffSummary(d []DiffLine) (added, removed int) {
	for _, l := range d {
		switch l.Op {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return added, removed
}

// RenderDiff 把 diff 渲染成人类可读文本（CLI 用）。
func RenderDiff(d []DiffLine) string {
	var b strings.Builder
	for _, l := range d {
		fmt.Fprintf(&b, "%c %s\n", l.Op, l.Text)
	}
	return b.String()
}

// RenderDiffHTML 把 diff 渲染成 HTML（Web UI 用，已转义）。
func RenderDiffHTML(d []DiffLine) string {
	var b strings.Builder
	for _, l := range d {
		cls := ""
		switch l.Op {
		case '+':
			cls = "d-add"
		case '-':
			cls = "d-del"
		}
		b.WriteString(`<div class="dline ` + cls + `">` + htmlEsc(string(l.Op)) + " " + htmlEsc(l.Text) + "</div>")
	}
	return b.String()
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func htmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
