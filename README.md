# AutoHostSwitch

**纯本地、零网络、单文件的跨平台 hosts 管理切换工具。**

手动改 hosts 的三大痛：改错一行全网瘫痪、找不到文件在哪、改前忘记备份。
AutoHostSwitch 一次解决：干净、轻量，一键快照、一键切换、一键还原。

```
┌─────────────────────────────────────────────┐
│  AutoHostSwitch                             │
│  1) 查看快照    3) 创建备份快照              │
│  2) 应用配置    4) 启动 Web 界面   5) 退出   │
└─────────────────────────────────────────────┘
```

UI 截图占位：`docs/screenshot-web.png`（Web 界面：编辑器 + 快照管理）
CLI 截图占位：`docs/screenshot-cli.png`（数字交互模式）

## 为什么比别的 hosts 工具干净

| | AutoHostSwitch | 常见同类工具 |
|---|---|---|
| 网络请求 | **零**（订阅手动拉取是唯一例外，默认关闭） | 自动更新、遥测上报常见 |
| 体积/依赖 | 单文件，零第三方依赖，无运行时 | Electron 动辄上百 MB |
| 写入保护 | 校验 → 自动安全快照 → 写入，缺一不可 | 直接覆盖，改错自认倒霉 |
| 后悔药 | 首次运行自动备份原始 hosts，随时一键还原 | 没有 |
| 后台驻留 | 无，跑完即退 | 常驻托盘/服务 |

## 一行启动教程

下载 [Releases](https://github.com/wangzi5151/autohostswitch/releases) 里对应系统的文件：

```sh
# Linux 64 位
chmod +x autohostswitch-linux-amd64 && sudo ./autohostswitch-linux-amd64

# macOS（Apple Silicon 示例）
chmod +x autohostswitch-darwin-arm64 && sudo ./autohostswitch-darwin-arm64

# 树莓派（32/64 位通用包）
chmod +x autohostswitch-linux-arm && sudo ./autohostswitch-linux-arm
```

Windows：右键 `autohostswitch-windows-amd64.exe` → **以管理员身份运行**（见下）。

Termux：见 [docs/TERMUX.md](docs/TERMUX.md) 专属教程。

无参数运行进入**数字交互模式**，跟着序号选就行，不用记命令：

```
1) 查看快照      2) 应用配置      3) 创建备份快照
4) 启动 Web 界面  5) 退出
```

也可以直接用命令（适合写脚本）：

```sh
sudo autohostswitch snapshot "改前备份"     # 一键快照
sudo autohostswitch apply 开发环境          # 一键切换配置
sudo autohostswitch restore-original       # 一键还原（救命用）
autohostswitch serve                        # 启动 Web 界面 -> http://127.0.0.1:8080
```

## 核心功能

- **自动识别 hosts 路径**：Windows / Linux / macOS / Termux 各取真实路径，也支持 `--hosts` 手动指定
- **快照系统**：无限快照，带备注和时间戳，可重命名/删除/导出单个；**每次写入前自动生成安全快照**
- **配置集**：多套 hosts 方案（开发环境/屏蔽广告/测试内网/重置默认），应用前做语法校验，坏文件拒绝写入
- **权限友好提示**：不够权限时给中文提示 + 可复制的提权命令，绝不自动强行提权
- **双模式**：完整 CLI（脚本/Termux/树莓派无人值守）+ 内置 Web UI（单文件前端，低饱和简洁风，编辑器带语法高亮与重复检测）
- **导出/导入**：整套快照+配置打成一个 JSON，换机同步，全程本地
- **紧急恢复**：首次运行自动备份的原始 hosts，一键还原
- **操作日志**：只记时间+动作，可一键清空，不收集设备信息
- **订阅（可选）**：手动点一次拉取一次，默认关闭，明确警告不可信来源；这是唯一允许的网络行为

## Windows 管理员注意事项

修改 `C:\Windows\System32\drivers\etc\hosts` 必须管理员权限：

1. 右键 exe →「以管理员身份运行」；或
2. 管理员 PowerShell：`Start-Process .\autohostswitch-windows-amd64.exe -Verb RunAs`

权限不足时工具会明确告诉你，并给出上面这段可复制的命令。

## 已知问题

- Go 1.21+ 工具链不再支持 Windows 7（运行时要求 Win10/Server 2016+），`windows-386` 包面向 32 位 Win10。Win7 用户请用旧版系统自带记事本手工改 hosts。
- macOS 首次运行可能弹「无法验证开发者」：系统设置 → 隐私与安全性 → 仍要打开。
- 部分杀软会拦截修改 hosts 的行为，属正常现象，加白即可。
- Android 未 root 时改不了 `/system/etc/hosts`，可用 `--hosts ~/hosts.test` 练手全部功能。

## 开源协议

MIT，详见 [LICENSE](LICENSE)。欢迎 PR（代码结构：`core` 核心逻辑 / `cli` 命令行 / `web` 内嵌 UI / `platform` 系统适配，重要位置均有中文注释）。

## 安全声明

本工具仅用于本地调试，禁止用于恶意劫持域名；无数据上传承诺。详见 [SECURITY.md](SECURITY.md)。
