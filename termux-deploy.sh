#!/bin/sh
# termux-deploy.sh — Android Termux 一键部署脚本。
# 在 Termux 里复制粘贴下面整段即可（共三行）：
#
#   curl -fsSL https://raw.githubusercontent.com/wangzi5151/autohostswitch/main/termux-deploy.sh | sh
#
# 做的事：装 Go → 拉源码 → 编译出 ~/bin/autohostswitch。
# 注意：修改系统 hosts（/system/etc/hosts）需要 root；
# 没 root 也能用：加 --hosts ~/hosts.test 先练手。

set -e
echo "== 1/3 安装依赖 =="
pkg update -y && pkg install -y golang git

echo "== 2/3 获取源码 =="
if [ -d "$HOME/autohostswitch" ]; then
  cd "$HOME/autohostswitch" && git pull --ff-only || true
else
  git clone https://github.com/wangzi5151/autohostswitch.git "$HOME/autohostswitch"
  cd "$HOME/autohostswitch"
fi

echo "== 3/3 编译 =="
mkdir -p "$HOME/bin"
go build -o "$HOME/bin/autohostswitch" .

echo ""
echo "完成！试试："
echo "  autohostswitch status"
echo ""
echo "没 root 的话先练手（不碰系统文件）："
echo "  autohostswitch --hosts ~/hosts.test status"
echo "有 root 要改真实 hosts："
echo "  su -c \"autohostswitch status\""
