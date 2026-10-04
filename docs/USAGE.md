# 详细用法

## 命令速查

```
autohostswitch [--hosts 路径] [--data 目录] <命令> [参数]

status                  状态：hosts 路径、是否可写、快照/配置数量
show                    打印当前 hosts
snapshot [名] [备注]     快照当前 hosts
snapshots               快照列表
rename-snapshot <旧> <新> [备注]
del-snapshot <名>        删除快照（原始备份删不掉）
restore <快照名>         从快照恢复
restore-original        紧急恢复：还原系统原始 hosts
profiles                配置列表
apply <配置名>           应用配置（一键切换）
add-profile <名> [--from 文件] [--from-current] [--note 备注]
rename-profile <旧> <新>
del-profile <名>
validate <文件>          只校验不写入（CI/脚本里好用）
export [备份.json]       导出全部（本地 JSON）
import <备份.json>       导入（重名自动改名）
log [--clear]            查看/清空操作日志
serve [--port 8080]      启动 Web UI（只绑 127.0.0.1）
subscribe <URL>          手动拉取订阅（默认关闭，需二次确认）
```

## 典型工作流

```sh
# 1. 改前先快照（好习惯）
sudo autohostswitch snapshot "2024大促前"

# 2. 建一套开发环境配置（从当前 hosts 起步再改）
sudo autohostswitch add-profile 开发环境 --from-current
# 然后去 Web 界面里编辑这套配置，或直接改文件后重新 add

# 3. 一键切换
sudo autohostswitch apply 开发环境     # 上班
sudo autohostswitch apply 屏蔽广告     # 摸鱼
sudo autohostswitch restore-original  # 兜底
```

## 校验规则

保存/应用前自动检查，**错误会阻止写入**，警告会提醒但放行：

- 错误：IP 格式不对、行里只有 IP 没有域名、域名含非法字符、域名某节以横杠开头/结尾、控制字符
- 警告：同一域名指向不同 IP、完全重复的行、域名含下划线（非标准）

`validate` 命令只做检查不写入，适合放进脚本做门禁：

```sh
autohostswitch validate ./my.hosts && echo OK
```

## 数据存在哪

| 系统 | 数据目录 |
|---|---|
| Windows | `%APPDATA%\AutoHostSwitch` |
| macOS | `~/Library/Application Support/AutoHostSwitch` |
| Linux/Termux | `~/.config/autohostswitch`（或 `$XDG_CONFIG_HOME`） |

里面是 `snapshots/`、`profiles/`、索引 JSON 和 `operations.log`，全是明文，
删掉即重置（下次运行会重新备份原始 hosts）。

`--data` 可指定别处，`--hosts` 可指定别的 hosts 路径（测试/绿色版/Termux 练手）。

## Web UI 说明

- 地址：`http://127.0.0.1:8080`（`--port` 可改端口，但只能是本地地址）
- 六个页签：当前 Hosts（编辑器带语法高亮+实时校验+重复检测）、快照、配置集、备份与恢复、日志、订阅
- 所有写操作与 CLI 走同一套 `core.ApplyHosts`：校验 → 自动安全快照 → 写入 → 日志
- 前端是单文件 `web/ui.html`，构建时 `go:embed` 打进二进制，无需 Node、无需构建步骤

## 订阅功能

默认关闭。CLI：`autohostswitch subscribe <URL>`；Web：在「订阅」页签操作。

- 只有你手动点才拉取一次，永不自动轮询
- 拉取后先预览 + 校验，必须二次确认才写入
- 警告：只填你完全信任的来源，恶意 hosts 可劫持域名
