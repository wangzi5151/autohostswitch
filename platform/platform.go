// Package platform 负责各操作系统的 hosts 真实路径识别、
// 写权限判断，以及权限不足时的人类可读中文提示（含可复制的提权命令）。
//
// 设计原则：绝不自动提权。只检测、只提示，把选择权留给用户。
package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HostsPath 返回当前系统 hosts 文件的真实路径。
//
// 优先级：
//  1. 环境变量 AUTOHOSTSWITCH_HOSTS（手动自定义路径，测试或特殊需求）
//  2. Windows: %SystemRoot%\System32\drivers\etc\hosts
//  3. Termux: /system/etc/hosts（Android 系统级，需要 root）
//  4. Linux / macOS: /etc/hosts
func HostsPath() string {
	if p := strings.TrimSpace(os.Getenv("AUTOHOSTSWITCH_HOSTS")); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		sysroot := os.Getenv("SystemRoot")
		if sysroot == "" {
			sysroot = `C:\Windows`
		}
		return filepath.Join(sysroot, "System32", "drivers", "etc", "hosts")
	}
	if IsTermux() {
		// Termux 没有自己独立的 hosts，真正生效的是 Android 系统的 hosts。
		return "/system/etc/hosts"
	}
	return "/etc/hosts"
}

// IsTermux 判断是否运行在 Android Termux 环境里。
func IsTermux() bool {
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	// $PREFIX 形如 /data/data/com.termux/files/usr
	return strings.Contains(os.Getenv("PREFIX"), "com.termux")
}

// OSName 返回给用户看的系统名字。
func OSName() string {
	if IsTermux() {
		return "Android Termux"
	}
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

// DataDir 返回本工具存放快照、配置、日志的本地数据目录。
// 可用 AUTOHOSTSWITCH_DATA 环境变量覆盖（方便测试与绿色版）。
func DataDir() string {
	if d := strings.TrimSpace(os.Getenv("AUTOHOSTSWITCH_DATA")); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "AutoHostSwitch")
		}
		return filepath.Join(home, "AutoHostSwitch")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "AutoHostSwitch")
	default:
		// Linux / Termux / 其他 Unix：优先 XDG，否则 ~/.config
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "autohostswitch")
		}
		return filepath.Join(home, ".config", "autohostswitch")
	}
}

// Writable 检测 path 是否可写。返回 (可写, 原因)。
// 用“实际尝试打开”的方式判断，比“猜身份”更诚实：
// 容器、ACL、只读挂载等边缘情况都能正确反映。
func Writable(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return true, nil
	}
	if os.IsPermission(err) {
		return false, err
	}
	// 文件不存在：检查父目录是否可写，顺带告诉用户路径有问题
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		pf, perr := os.OpenFile(filepath.Join(parent, ".ahs_write_test"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if perr == nil {
			pf.Close()
			os.Remove(filepath.Join(parent, ".ahs_write_test"))
			return false, fmt.Errorf("hosts 文件不存在：%s", path)
		}
		return false, fmt.Errorf("hosts 文件不存在，且目录也不可写：%s", parent)
	}
	return false, err
}

// ElevateHint 返回当前系统下可复制即用的提权命令提示。
func ElevateHint(prog string) string {
	if prog == "" {
		prog = "autohostswitch"
	}
	switch {
	case runtime.GOOS == "windows":
		return "请用管理员身份重新运行：\n" +
			"  1. 右键 " + prog + ".exe →「以管理员身份运行」；或\n" +
			"  2. 在管理员 PowerShell 里执行：\n" +
			`     Start-Process .` + string(filepath.Separator) + prog + `.exe -Verb RunAs`
	case IsTermux():
		return "Termux 下修改系统 hosts 需要 root：\n" +
			"  su -c \"" + prog + " apply <配置名>\"\n" +
			"（或先执行 su 进入 root shell 再运行；未 root 的手机只能读写自定义路径，\n" +
			"   用 --hosts 参数指向一个测试文件练习，例如 --hosts ~/hosts.test）"
	default:
		// Linux / macOS
		return "需要管理员权限，请用 sudo 重新运行，例如：\n" +
			"  sudo " + prog + " apply <配置名>"
	}
}

// FriendlyWriteError 把写入 hosts 时的常见系统错误翻译成人类中文，
// 并附带“新手一行恢复命令”。绝不直接抛原始报错或堆栈。
func FriendlyWriteError(err error, prog string) string {
	if err == nil {
		return ""
	}
	hostsPath := HostsPath()
	msg := err.Error()

	// 磁盘满
	if errors.Is(err, errDiskFull()) || containsAny(msg, []string{"no space left", "disk full", "空间不足"}) {
		return "写入失败：磁盘空间不足。\n请清理磁盘后再试，你的 hosts 文件没有被改动。"
	}
	// 只读文件系统
	if containsAny(msg, []string{"read-only file system", "只读"}) {
		return "写入失败：文件系统是只读的（常见于未 remount 的 Android /system）。\n" +
			"Termux 用户可尝试：mount -o remount,rw /system（需要 root）"
	}
	// 文件被占用（Windows 常见）
	if containsAny(msg, []string{"being used", "另一个程序"}) {
		return "写入失败：hosts 文件正被其他程序占用。\n请关闭可能锁定它的安全软件/编辑器后重试。"
	}
	// 权限不足：主路径
	if os.IsPermission(err) {
		return "权限不足：当前身份写不了 hosts 文件。\n\n" +
			ElevateHint(prog) + "\n\n新手一行恢复（提权后执行即可还原）：\n" +
			"  " + prog + " restore-original"
	}
	// 路径不存在
	if os.IsNotExist(err) {
		return "找不到 hosts 文件：" + hostsPath + "\n" +
			"请检查路径是否正确，或用 --hosts 手动指定：\n" +
			"  " + prog + " --hosts <你的hosts路径> status"
	}
	// 兜底：给中文包装，不暴露堆栈
	return "写入失败：" + msg + "\n如需帮助，请把上面这行信息发给开发者（不含任何个人数据）。"
}

// errDiskFull 返回一个用于 errors.Is 比对的哨兵错误。
// Go 标准库没有导出 ENOSPC 的跨平台哨兵，这里用字符串兜底，
// 上面的 containsAny 已经覆盖了真实报错文本。
func errDiskFull() error { return errors.New("no space left on device") }

func containsAny(s string, subs []string) bool {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(low, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
