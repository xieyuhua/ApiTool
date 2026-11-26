#!/usr/bin/env bash
# 构建独立 AI Agent 服务（agentd）。
#
# 步骤：
#   1) 构建前端（frontend/dist，包含桌面端与 Agent 两个入口）
#   2) 把产物复制到 agent/webui/dist，供 //go:embed 嵌入
#   3) 交叉编译 6 个平台的单文件二进制（无 CGO）
#
# 用法：
#   ./build-agent.sh            # 全平台
#   ./build-agent.sh windows    # 只构建指定平台（linux|darwin|windows）
set -euo pipefail

cd "$(dirname "$0")"

VERSION="${VERSION:-1.0.0}"
OUT="${OUT:-dist}"
TARGETS="${1:-all}"

echo "==> 构建前端"
if [ ! -d frontend/node_modules ]; then
  (cd frontend && npm install)
fi
(cd frontend && npm run build)

echo "==> 复制前端产物到 agent/webui/dist"
rm -rf agent/webui/dist
mkdir -p agent/webui/dist
cp -r frontend/dist/. agent/webui/dist/
touch agent/webui/dist/.gitkeep

build() {
  local goos=$1 goarch=$2
  local name="agentd-${goos}-${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"
  echo "--> $name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -tags purego -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT}/${name}" ./agent/cmd/agentd
}

mkdir -p "$OUT"

case "$TARGETS" in
  linux)   build linux amd64;  build linux arm64 ;;
  darwin)  build darwin amd64; build darwin arm64 ;;
  windows) build windows amd64; build windows arm64 ;;
  all)
    build linux amd64;  build linux arm64
    build darwin amd64; build darwin arm64
    build windows amd64; build windows arm64
    ;;
  *)
    echo "未知目标：$TARGETS（可选 all|linux|darwin|windows）" >&2
    exit 1
    ;;
esac

echo
echo "==> 完成，产物位于 ${OUT}/"
ls -lh "$OUT" | tail -n +2
