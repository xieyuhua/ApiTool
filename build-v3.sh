#!/usr/bin/env bash
# Wails v3 构建脚本（不依赖 Task 命令）。
#
#   bash build-v3.sh          # 生产构建：GUI 子系统、去符号，输出 build/bin/apitool.exe
#   bash build-v3.sh debug    # 调试构建：保留控制台输出与调试信息
#
# 说明：前端沿用 v2 的 wailsjs 调用形态，由 src/wailsV3Bridge.js 在运行时
# 映射到 v3 的 Call.ByName / Events，因此无需 wails3 生成绑定。
set -euo pipefail

cd "$(dirname "$0")"

MODE="${1:-production}"
APP_NAME="apitool"
OUT="build/bin/${APP_NAME}.exe"

echo "==> 构建前端"
( cd frontend && npm install --silent && npm run build )

echo "==> 构建后端（${MODE}）"
if [ "$MODE" = "debug" ]; then
  # 调试版：保留控制台输出，便于查看日志
  CGO_ENABLED=0 go build -buildvcs=false -gcflags='all=-l' -o "$OUT" .
else
  CGO_ENABLED=0 go build -tags production -trimpath -buildvcs=false \
    -ldflags='-w -s -H windowsgui' -o "$OUT" .
fi

echo "==> 完成：$OUT"
ls -lh "$OUT"
