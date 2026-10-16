#!/usr/bin/env bash
# 开发模式：挂载源码 + go run，改完代码 Ctrl+C 重启即生效，不 build 镜像。
# 前提：先退出生产容器（bash scripts/aide.sh stop）。
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"
source scripts/launcher-data.sh
launcher_env_restore

echo "启动 dev 容器（Go + Python + Node，源码 go run）…"
docker compose -f compose.yaml -f compose.dev.yaml build aide
launcher_data_prepare docker aide:dev
launcher_env_save
rm -f .cache/aide-manual-stop
exec docker compose -f compose.yaml -f compose.dev.yaml up --no-build --force-recreate
