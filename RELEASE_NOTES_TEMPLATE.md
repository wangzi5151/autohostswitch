# Release 发布说明模板

<!-- 打 tag vX.Y.Z 后，release 工作流会自动编译 7 个平台包并附上 SHA256SUMS。
     把下面模板填好粘到 Release 正文。 -->

## AutoHostSwitch vX.Y.Z

默认纯本地、默认零网络、单文件跨平台的 hosts 管理切换工具。

### 下载

根据你的系统下载对应文件，解压即用，无需安装：

| 系统 | 文件 |
|---|---|
| Windows 64 位 | `autohostswitch-windows-amd64.exe` |
| Windows 32 位 | `autohostswitch-windows-386.exe` |
| Linux 64 位 | `autohostswitch-linux-amd64` |
| Linux ARM64 | `autohostswitch-linux-arm64` |
| 树莓派 / 32 位 ARM | `autohostswitch-linux-arm` |
| macOS Intel | `autohostswitch-darwin-amd64` |
| macOS Apple Silicon | `autohostswitch-darwin-arm64` |

校验完整性：`sha256sum -c SHA256SUMS`（Windows 可用 `certutil -hashfile 文件 SHA256`）。

### 一行启动

```sh
# Linux / macOS / 树莓派
chmod +x autohostswitch-linux-amd64 && sudo ./autohostswitch-linux-amd64

# Termux（见 docs/TERMUX.md）
curl -fsSL https://raw.githubusercontent.com/wangzi5151/autohostswitch/main/termux-deploy.sh | sh
```

### 本次更新

- ...
- ...

### 安全提醒

- Windows 请右键「以管理员身份运行」；Linux/macOS 请加 `sudo`；Termux 改系统 hosts 需要 root。
- 本工具仅用于本地调试，禁止用于恶意劫持域名。详见 SECURITY.md。
