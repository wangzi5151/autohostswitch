// Package cli 实现命令行入口：完整子命令 + 无参数时的数字交互模式。
// 所有报错均为中文人类可读，不抛堆栈。
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/wangzi5151/autohostswitch/core"
	"github.com/wangzi5151/autohostswitch/platform"
	"github.com/wangzi5151/autohostswitch/web"
)

// Run 是 CLI 入口。args 为 os.Args[1:]。
func Run(args []string, version string) int {
	// 全局 flag：--hosts / --data / --version / --help（可出现在子命令之前）
	var hostsPath, dataDir string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--hosts" && i+1 < len(args):
			i++
			hostsPath = args[i]
		case strings.HasPrefix(a, "--hosts="):
			hostsPath = strings.TrimPrefix(a, "--hosts=")
		case a == "--data" && i+1 < len(args):
			i++
			dataDir = args[i]
		case strings.HasPrefix(a, "--data="):
			dataDir = strings.TrimPrefix(a, "--data=")
		case a == "--version" || a == "-v":
			fmt.Println("autohostswitch", version)
			return 0
		case a == "--help" || a == "-h":
			printHelp()
			return 0
		default:
			rest = append(rest, a)
		}
	}
	if hostsPath != "" {
		os.Setenv("AUTOHOSTSWITCH_HOSTS", hostsPath)
	}
	if dataDir != "" {
		os.Setenv("AUTOHOSTSWITCH_DATA", dataDir)
	}

	if len(rest) == 0 {
		if isTerminal() {
			return interactive(version)
		}
		printHelp()
		return 0
	}

	store, err := core.NewStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败："+err.Error())
		return 1
	}
	// 首次运行顺手种一颗“重置默认”配置
	_ = store.SeedDefaultProfile()

	cmd, cargs := rest[0], rest[1:]
	switch cmd {
	case "status":
		return cmdStatus(store, version)
	case "show":
		return cmdShow(store)
	case "snapshot", "snap":
		return cmdSnapshot(store, cargs)
	case "snapshots", "list":
		return cmdListSnapshots(store)
	case "restore":
		return cmdRestore(store, cargs)
	case "restore-original":
		return cmdRestoreOriginal(store)
	case "profiles":
		return cmdListProfiles(store)
	case "apply":
		return cmdApply(store, cargs)
	case "add-profile":
		return cmdAddProfile(store, cargs)
	case "del-profile":
		return cmdDelProfile(store, cargs)
	case "rename-profile":
		return cmdRenameProfile(store, cargs)
	case "rename-snapshot":
		return cmdRenameSnapshot(store, cargs)
	case "del-snapshot":
		return cmdDelSnapshot(store, cargs)
	case "export":
		return cmdExport(store, cargs)
	case "import":
		return cmdImport(store, cargs)
	case "log":
		return cmdLog(store, cargs)
	case "validate":
		return cmdValidate(cargs)
	case "serve", "web", "ui":
		return cmdServe(store, version, cargs)
	case "subscribe":
		return cmdSubscribe(store, cargs)
	case "help", "--help", "-h":
		printHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "不认识的命令「%s」，看看帮助：\n\n", cmd)
		printHelp()
		return 1
	}
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "失败："+format+"\n", a...)
	return 1
}

func ok(format string, a ...any) int {
	fmt.Printf(format+"\n", a...)
	return 0
}

// ---------- 子命令 ----------

func cmdStatus(store *core.Store, version string) int {
	writable, _ := platform.Writable(store.HostsPath)
	snaps, _ := store.ListSnapshots()
	profs, _ := store.ListProfiles()
	fmt.Printf("AutoHostSwitch %s\n", version)
	fmt.Printf("系统：%s\n", platform.OSName())
	fmt.Printf("hosts 路径：%s（%s）\n", store.HostsPath, map[bool]string{true: "可写", false: "只读，需要提权"}[writable])
	fmt.Printf("数据目录：%s\n", store.DataDir)
	fmt.Printf("快照：%d 个　配置：%d 套\n", len(snaps), len(profs))
	if !writable {
		fmt.Printf("\n%s\n", platform.ElevateHint("autohostswitch"))
	}
	return 0
}

func cmdShow(store *core.Store) int {
	content, err := store.ReadCurrentHosts()
	if err != nil {
		return fail("%s", err.Error())
	}
	fmt.Print(content)
	return 0
}

func cmdSnapshot(store *core.Store, args []string) int {
	name, note := "", ""
	if len(args) > 0 {
		name = args[0]
	}
	if len(args) > 1 {
		note = strings.Join(args[1:], " ")
	}
	snap, err := store.CreateSnapshot(name, note)
	if err != nil {
		return fail("%s", err.Error())
	}
	return ok("快照已创建：「%s」（%s）", snap.Name, snap.CreatedAt)
}

func cmdListSnapshots(store *core.Store) int {
	list, err := store.ListSnapshots()
	if err != nil {
		return fail("%s", err.Error())
	}
	if len(list) == 0 {
		fmt.Println("还没有快照。用 snapshot 命令创建一个吧。")
		return 0
	}
	orig := store.OriginalSnapshotID()
	for _, s := range list {
		marks := ""
		if s.ID == orig {
			marks += " [原始备份]"
		}
		if s.Auto {
			marks += " [自动]"
		}
		fmt.Printf("· %s%s\n  %s  %s\n", s.Name, marks, s.CreatedAt, s.Note)
	}
	return 0
}

func cmdRestore(store *core.Store, args []string) int {
	if len(args) == 0 {
		return fail("用法：autohostswitch restore <快照名或ID>")
	}
	if err := store.RestoreSnapshot(args[0]); err != nil {
		if core.IsApplyWarnings(err) {
			fmt.Println(err.Error())
			return 0
		}
		return fail("%s", friendly(err))
	}
	return ok("已从快照恢复 hosts。")
}

func cmdRestoreOriginal(store *core.Store) int {
	if err := store.RestoreOriginal(); err != nil {
		if core.IsApplyWarnings(err) {
			fmt.Println(err.Error())
			return 0
		}
		return fail("%s", friendly(err))
	}
	return ok("已恢复为系统原始 hosts。")
}

func cmdListProfiles(store *core.Store) int {
	list, err := store.ListProfiles()
	if err != nil {
		return fail("%s", err.Error())
	}
	if len(list) == 0 {
		fmt.Println("还没有配置。用 add-profile 新建一套吧。")
		return 0
	}
	for _, p := range list {
		fmt.Printf("· %s\n  %s  更新于 %s\n", p.Name, p.Note, p.UpdatedAt)
	}
	return 0
}

func cmdApply(store *core.Store, args []string) int {
	if len(args) == 0 {
		return fail("用法：autohostswitch apply <配置名>")
	}
	if err := store.ApplyProfile(args[0]); err != nil {
		if core.IsApplyWarnings(err) {
			fmt.Println(err.Error())
			return 0
		}
		return fail("%s", friendly(err))
	}
	return ok("配置「%s」已应用到 hosts。", args[0])
}

// cmdAddProfile 支持三种来源：--from 文件 / --from-current / 交互输入。
func cmdAddProfile(store *core.Store, args []string) int {
	var name, from, note string
	fromCurrent := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--from" && i+1 < len(args):
			i++
			from = args[i]
		case strings.HasPrefix(args[i], "--from="):
			from = strings.TrimPrefix(args[i], "--from=")
		case args[i] == "--from-current":
			fromCurrent = true
		case args[i] == "--note" && i+1 < len(args):
			i++
			note = args[i]
		case !strings.HasPrefix(args[i], "-") && name == "":
			name = args[i]
		}
	}
	if name == "" {
		name = prompt("配置名（例如：开发环境）：")
		if name == "" {
			return fail("配置名不能为空。")
		}
	}
	var content string
	switch {
	case fromCurrent:
		c, err := store.ReadCurrentHosts()
		if err != nil {
			return fail("%s", err.Error())
		}
		content = c
	case from != "":
		data, err := os.ReadFile(from)
		if err != nil {
			return fail("读文件失败：%s", err.Error())
		}
		content = string(data)
	default:
		fmt.Println("粘贴 hosts 内容，输完后按 Ctrl+D 结束：")
		data, err := ioReadAll(os.Stdin)
		if err != nil {
			return fail("读取输入失败：%s", err.Error())
		}
		content = string(data)
	}
	if _, err := store.CreateProfile(name, note, content); err != nil {
		if verr, ok := err.(*core.ValidationError); ok {
			return fail("%s", verr.Error())
		}
		return fail("%s", err.Error())
	}
	return ok("配置「%s」已创建。", name)
}

func cmdDelProfile(store *core.Store, args []string) int {
	if len(args) == 0 {
		return fail("用法：autohostswitch del-profile <配置名>")
	}
	if err := store.DeleteProfile(args[0]); err != nil {
		return fail("%s", err.Error())
	}
	return ok("配置「%s」已删除。", args[0])
}

func cmdRenameProfile(store *core.Store, args []string) int {
	if len(args) < 2 {
		return fail("用法：autohostswitch rename-profile <旧名> <新名>")
	}
	if err := store.RenameProfile(args[0], args[1]); err != nil {
		return fail("%s", err.Error())
	}
	return ok("已重命名为「%s」。", args[1])
}

func cmdRenameSnapshot(store *core.Store, args []string) int {
	if len(args) < 2 {
		return fail("用法：autohostswitch rename-snapshot <旧名> <新名> [备注]")
	}
	note := ""
	if len(args) > 2 {
		note = strings.Join(args[2:], " ")
	}
	if err := store.RenameSnapshot(args[0], args[1], note); err != nil {
		return fail("%s", err.Error())
	}
	return ok("快照已重命名为「%s」。", args[1])
}

func cmdDelSnapshot(store *core.Store, args []string) int {
	if len(args) == 0 {
		return fail("用法：autohostswitch del-snapshot <快照名或ID>")
	}
	if err := store.DeleteSnapshot(args[0]); err != nil {
		return fail("%s", err.Error())
	}
	return ok("快照已删除。")
}

func cmdExport(store *core.Store, args []string) int {
	path := "autohostswitch-backup.json"
	if len(args) > 0 {
		path = args[0]
	}
	if err := store.ExportAllToFile(path); err != nil {
		return fail("%s", err.Error())
	}
	return ok("已导出到 %s（纯本地文件，没有上传任何地方）。", path)
}

func cmdImport(store *core.Store, args []string) int {
	if len(args) == 0 {
		return fail("用法：autohostswitch import <备份.json>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fail("读备份文件失败：%s", err.Error())
	}
	ns, np, err := store.ImportAll(data)
	if err != nil {
		return fail("%s", err.Error())
	}
	return ok("导入成功：%d 个快照，%d 个配置（重名的已自动改名，不覆盖）。", ns, np)
}

func cmdLog(store *core.Store, args []string) int {
	if len(args) > 0 && args[0] == "--clear" {
		if err := store.ClearLog(); err != nil {
			return fail("%s", err.Error())
		}
		return ok("日志已清空。")
	}
	lines, err := store.ReadLog()
	if err != nil {
		return fail("%s", err.Error())
	}
	if len(lines) == 0 {
		fmt.Println("（暂无日志）")
		return 0
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return 0
}

// cmdValidate 校验一个 hosts 文件，不写入。
func cmdValidate(args []string) int {
	if len(args) == 0 {
		return fail("用法：autohostswitch validate <文件路径>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fail("读文件失败：%s", err.Error())
	}
	issues := core.ValidateHosts(string(data))
	if len(issues) == 0 {
		return ok("校验通过：没有发现问题。")
	}
	for _, it := range issues {
		kind := "错误"
		if it.Warn {
			kind = "警告"
		}
		if it.Line > 0 {
			fmt.Printf("[%s] 第 %d 行：%s\n", kind, it.Line, it.Msg)
		} else {
			fmt.Printf("[%s] %s\n", kind, it.Msg)
		}
	}
	if core.HasError(issues) {
		return 1
	}
	return 0
}

func cmdServe(store *core.Store, version string, args []string) int {
	addr := "127.0.0.1:8080"
	for i := 0; i < len(args); i++ {
		if args[i] == "--port" && i+1 < len(args) {
			i++
			addr = args[i]
		} else if strings.HasPrefix(args[i], "--port=") {
			addr = strings.TrimPrefix(args[i], "--port=")
		}
	}
	// 允许 8080 或 127.0.0.1:8080 两种写法
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	}
	fmt.Printf("AutoHostSwitch Web UI 启动中…\n本地访问：http://%s\n按 Ctrl+C 停止。\n", addr)
	srv := web.NewServer(store, version)
	if err := srv.ListenAndServe(addr); err != nil {
		return fail("%s", err.Error())
	}
	return 0
}

// cmdSubscribe 手动拉取订阅（唯一允许的网络行为，默认关闭）。
func cmdSubscribe(store *core.Store, args []string) int {
	if len(args) == 0 {
		fmt.Println(core.SubscribeWarning)
		fmt.Println("\n用法：autohostswitch subscribe <URL>")
		return 0
	}
	fmt.Println(core.SubscribeWarning)
	fmt.Print("\n我已阅读并确认来源可信，继续拉取？(y/N)：")
	if !confirm() {
		fmt.Println("已取消。")
		return 0
	}
	content, err := core.PullSubscription(args[0])
	if err != nil {
		return fail("%s", err.Error())
	}
	issues := core.ValidateHosts(content)
	if core.HasError(issues) {
		fmt.Println("拉取到的内容校验没通过，拒绝写入：")
		for _, it := range issues {
			if !it.Warn {
				fmt.Printf("  第 %d 行：%s\n", it.Line, it.Msg)
			}
		}
		return 1
	}
	fmt.Printf("拉取成功（%d 字节），校验通过。\n", len(content))
	fmt.Print("确认写入 hosts？(y/N)：")
	if !confirm() {
		fmt.Println("已取消，内容未写入。")
		return 0
	}
	if err := store.ApplyHosts(content, "应用订阅 "+args[0]); err != nil {
		if core.IsApplyWarnings(err) {
			fmt.Println(err.Error())
			return 0
		}
		return fail("%s", friendly(err))
	}
	return ok("订阅已应用到 hosts。")
}

// ---------- 数字交互模式 ----------

func interactive(version string) int {
	store, err := core.NewStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败："+err.Error())
		return 1
	}
	_ = store.SeedDefaultProfile()

	writable, _ := platform.Writable(store.HostsPath)
	fmt.Printf("AutoHostSwitch %s（%s）\n", version, platform.OSName())
	fmt.Printf("hosts：%s（%s）\n\n", store.HostsPath, map[bool]string{true: "可写", false: "只读，需提权"}[writable])

	for {
		fmt.Println("1) 查看快照")
		fmt.Println("2) 应用配置")
		fmt.Println("3) 创建备份快照")
		fmt.Println("4) 启动 Web 界面")
		fmt.Println("5) 退出")
		choice := prompt("请选择 [1-5]：")
		switch choice {
		case "1":
			cmdListSnapshots(store)
		case "2":
			profs, err := store.ListProfiles()
			if err != nil {
				fmt.Println("失败：" + err.Error())
				continue
			}
			if len(profs) == 0 {
				fmt.Println("还没有配置，先用 add-profile 新建一套。")
				continue
			}
			for i, p := range profs {
				fmt.Printf("  %d) %s\n", i+1, p.Name)
			}
			sel := prompt("选哪套（输入序号）：")
			idx := atoi(sel) - 1
			if idx < 0 || idx >= len(profs) {
				fmt.Println("序号不对。")
				continue
			}
			if err := store.ApplyProfile(profs[idx].Name); err != nil {
				if core.IsApplyWarnings(err) {
					fmt.Println(err.Error())
				} else {
					fmt.Println("失败：" + friendly(err))
				}
				continue
			}
			fmt.Printf("配置「%s」已应用。\n", profs[idx].Name)
		case "3":
			note := prompt("备注（可空，直接回车跳过）：")
			snap, err := store.CreateSnapshot("", note)
			if err != nil {
				fmt.Println("失败：" + err.Error())
				continue
			}
			fmt.Printf("快照「%s」已创建。\n", snap.Name)
		case "4":
			fmt.Println("正在启动 Web 界面…")
			return cmdServe(store, version, []string{"--port", "8080"})
		case "5", "q", "Q":
			fmt.Println("再见。")
			return 0
		default:
			fmt.Println("请输入 1-5。")
		}
		fmt.Println()
	}
}

// ---------- 小工具 ----------

func printHelp() {
	fmt.Print(`AutoHostSwitch — 纯本地 hosts 管理切换工具（零网络、零埋点）

用法：autohostswitch [--hosts 路径] [--data 目录] <命令> [参数]

命令：
  status                  显示 hosts 路径、权限、快照/配置数量
  show                    打印当前 hosts 内容
  snapshot [名] [备注]     把当前 hosts 存成快照
  snapshots               列出全部快照
  rename-snapshot <旧> <新> [备注]
  del-snapshot <名>        删除快照（原始备份删不掉）
  restore <快照名>         从快照恢复 hosts
  restore-original        紧急恢复：还原为系统原始 hosts
  profiles                列出全部配置集
  apply <配置名>           一键应用配置（自动校验+自动安全快照）
  add-profile <名> [--from 文件] [--from-current] [--note 备注]
  rename-profile <旧> <新>
  del-profile <名>
  validate <文件>          只校验不写入
  export [备份.json]       导出全部快照+配置（本地 JSON）
  import <备份.json>       导入备份（重名自动改名）
  log [--clear]            查看 / 清空操作日志
  serve [--port 8080]      启动内置 Web UI（只绑 127.0.0.1）
  subscribe <URL>          手动拉取订阅（默认关闭，需二次确认）
  help                    显示本帮助

无参数直接运行会进入数字交互模式（1查 2用 3备 4网 5退）。
`)
}

// friendly 把写入类错误转成中文（ApplyHosts 内部已处理过权限，这里兜底）。
func friendly(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func prompt(label string) string {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func confirm() bool {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func ioReadAll(f *os.File) ([]byte, error) {
	var b strings.Builder
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		b.WriteString(line)
		if err != nil {
			break
		}
	}
	return []byte(b.String()), nil
}
