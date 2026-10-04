package core

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 订阅功能是本工具唯一允许的网络行为，且：
//   - 默认关闭：不配置 URL 时整个功能不可用
//   - 绝不自动轮询：只有用户手动点“拉取”才发一次请求
//   - 拉取后只做校验 + 预览，必须用户二次确认才会写入
//
// 警告：不要填入不可信来源。恶意 hosts 可劫持域名，非常危险。

// subscribeClient 是订阅专用的 HTTP 客户端：短超时、限大小。
var subscribeClient = &http.Client{Timeout: 20 * time.Second}

// PullSubscription 手动拉取一次订阅 URL 的文本内容。
// 返回内容；调用方必须再走 ValidateHosts + ApplyHosts 流程。
func PullSubscription(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("订阅 URL 为空，功能未启用")
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", fmt.Errorf("订阅 URL 必须以 http:// 或 https:// 开头（建议只用 https）")
	}
	resp, err := subscribeClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("拉取订阅失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("订阅服务器返回了 %s", resp.Status)
	}
	// 限 2MB，防止误填大文件把内存撑爆
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("读取订阅内容失败：%w", err)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("订阅内容是空的")
	}
	return text, nil
}

// SubscribeWarning 是展示给用户的固定警告文案，CLI 与 Web 共用。
const SubscribeWarning = "警告：订阅会把远程文本写入你的 hosts。" +
	"只填写你完全信任的来源（如你自己的服务器），不要填来路不明的 URL，" +
	"恶意 hosts 可以劫持域名、窃取账号。本功能默认关闭，不会自动更新。"
