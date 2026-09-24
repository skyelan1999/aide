#!/usr/bin/env bash
# 开发模式：挂载源码 + go run，改完代码 Ctrl+C 重启即生效，不 build 镜像。
# 前提：先退出生产容器（bash scripts/aide.sh stop）。
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"

if docker compose ps --status running 2>/dev/null | grep -q aide; then
  echo "检测到生产容器在跑，先停止…"
  bash scripts/aide.sh stop
fi

echo "启动 dev 容器（golang + go run，首次编译约 5-10 秒）…"
exec docker compose -f compose.yaml -f compose.dev.yaml up --build=false --force-recreate
