# 快速上手

## 树莓派（Raspberry Pi OS）

```sh
# 1. 下载对应包（64 位系统用 arm64，32 位用 arm）
wget https://github.com/wangzi5151/autohostswitch/releases/latest/download/autohostswitch-linux-arm64
# 32 位系统换成：autohostswitch-linux-arm

# 2. 加执行权限并运行
chmod +x autohostswitch-linux-arm64
sudo ./autohostswitch-linux-arm64
```

无人值守写法（写进 cron 或脚本）：

```sh
sudo /home/pi/autohostswitch-linux-arm64 apply 广告屏蔽
sudo /home/pi/autohostswitch-linux-arm64 snapshot "每周备份"
```

## Android Termux

### 一键部署（复制整段粘贴）

```sh
curl -fsSL https://raw.githubusercontent.com/wangzi5151/autohostswitch/main/termux-deploy.sh | sh
```

这会安装 Go、拉源码、在 Termux 里编译出 `~/bin/autohostswitch`。

### 没 root 的手机（大多数）

Android 系统的 hosts 在 `/system/etc/hosts`，没 root 写不了。
但**全部功能都能练手**——用一个测试文件代替：

```sh
# 先造个假 hosts 练手
echo "127.0.0.1 localhost" > ~/hosts.test

# 之后每次加 --hosts 参数即可
autohostswitch --hosts ~/hosts.test status
autohostswitch --hosts ~/hosts.test snapshot "练习快照"
autohostswitch --hosts ~/hosts.test apply 开发环境
autohostswitch --hosts ~/hosts.test serve --port 8080
# 浏览器打开 http://127.0.0.1:8080 玩 Web 界面
```

### 有 root 的手机

```sh
# 方式一：整条命令提权
su -c "autohostswitch apply 开发环境"

# 方式二：先进 root shell 再随便玩
su
autohostswitch
```

> 注意：部分 ROM 的 `/system` 是只读挂载，提示只读时先执行
> `mount -o remount,rw /system`（需要 root），改完建议改回只读。

## Linux / macOS 通用

```sh
chmod +x autohostswitch-linux-amd64
sudo ./autohostswitch-linux-amd64 status   # 先看状态，确认路径和权限
sudo ./autohostswitch-linux-amd64          # 数字交互模式
```

Web 界面（本机浏览器打开 `http://127.0.0.1:8080`）：

```sh
sudo autohostswitch serve --port 8080
```

只绑 127.0.0.1，不会暴露到局域网；改 `--port` 也只能是本地地址。

## 新手一行恢复命令

不管怎么改错，只要还能运行本工具（提权后）：

```sh
sudo autohostswitch restore-original
```

这会把 hosts 还原为**第一次运行时自动备份的原始版本**。改之前工具也会自动再存一份安全快照，双保险。
