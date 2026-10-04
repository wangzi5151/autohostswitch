#!/bin/bash
# build-all.sh — 一键编译 AutoHostSwitch 全部目标平台。
# 用法：./build-all.sh [版本号]，例如 ./build-all.sh v0.1.0
# 产物在 dist/ 目录，命名：autohostswitch-<os>-<arch>[.exe]
set -euo pipefail

VERSION="${1:-dev}"
OUT="dist"
LDFLAGS="-s -w -X main.version=${VERSION}"

# 目标平台：Windows(amd64/386)、Linux(amd64/arm64/armv7 树莓派)、macOS(amd64/arm64)
TARGETS=(
  "windows/amd64"
  "windows/386"
  "linux/amd64"
  "linux/arm64"
  "linux/armv7"
  "darwin/amd64"
  "darwin/arm64"
)

mkdir -p "$OUT"
echo "Building AutoHostSwitch ${VERSION} ..."

for t in "${TARGETS[@]}"; do
  os="${t%%/*}"
  arch="${t##*/}"
  # GOARM 仅对 32 位 ARM 生效
  goarm=""
  if [ "$arch" = "armv7" ]; then
    arch="arm"
    goarm="7"
  fi
  name="autohostswitch-${os}-${arch}"
  [ "$os" = "windows" ] && name="${name}.exe"
  echo "  -> $name"
  # 注意：不能写成 VAR=... ${cond:+VAR2=...} cmd 的形式，
  # bash 在展开前按字面判定赋值词，展开出的 GOARM=7 会被当成命令名。
  # 用 env 统一传环境变量则无此问题。
  env CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" ${goarm:+GOARM="$goarm"} \
    go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$name" .
done

echo "OK: $(ls "$OUT" | wc -l) 个文件已生成到 $OUT/"
ls "$OUT"
