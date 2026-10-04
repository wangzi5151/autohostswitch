package core

import (
	"fmt"
	"strings"
)

// DiffOp 是 diff 行的操作类型：' ' 不变，'+' 新增，'-' 删除。
type DiffLine struct {
	Op      byte   // ' ', '+', '-'
	Text    string // 行内容（不含换行符）
	OldLine int    // 在旧文本中的行号（1 起；新增行为 0）
	NewLine int    // 在新文本中的行号（1 起；删除行为 0）
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
			out = append(out, DiffLine{' ', a[i], i + 1, j + 1})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, DiffLine{'-', a[i], i + 1, 0})
			i++
		default:
			out = append(out, DiffLine{'+', b[j], 0, j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{'-', a[i], i + 1, 0})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{'+', b[j], 0, j + 1})
	}
	return out
}

// DiffStats 是 diff 的统计：新增 / 删除 / 修改。
// “修改”指相邻的“删+增”配对（同一位置一行被替换），
// 即 dels/增配对后剩下的才分别计入删除/新增。
type DiffStats struct {
	Added    int
	Removed  int
	Modified int
}

// DiffSummary 统计 diff，给确认框和 Web UI 用。
func DiffSummary(d []DiffLine) DiffStats {
	var st DiffStats
	i := 0
	for i < len(d) {
		if d[i].Op == '-' {
			// 连续的删除块
			j := i
			for j < len(d) && d[j].Op == '-' {
				j++
			}
			// 紧随其后的连续新增块
			k := j
			for k < len(d) && d[k].Op == '+' {
				k++
			}
			dels, adds := j-i, k-j
			pair := dels
			if adds < pair {
				pair = adds
			}
			st.Modified += pair
			st.Removed += dels - pair
			st.Added += adds - pair
			i = k
		} else if d[i].Op == '+' {
			st.Added++
			i++
		} else {
			i++
		}
	}
	return st
}

// lineNo 把行号格式化成右对齐宽度，无行号时留空。
func lineNo(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*d", width, n)
}

// RenderDiff 把 diff 渲染成人类可读文本（CLI 用），带行号：
//
//	12  |   12 |   127.0.0.1 localhost
//	13  |-     | - 127.0.0.2 old.example
//	    |+  14 | + 127.0.0.2 new.example
func RenderDiff(d []DiffLine) string {
	w := 1
	for _, l := range d {
		if l.OldLine > w {
			w = len(fmt.Sprint(l.OldLine))
		}
		if l.NewLine > w {
			w = len(fmt.Sprint(l.NewLine))
		}
	}
	var b strings.Builder
	for _, l := range d {
		fmt.Fprintf(&b, "%s %c %s | %c %s\n",
			lineNo(l.OldLine, w), l.Op, lineNo(l.NewLine, w), l.Op, l.Text)
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
