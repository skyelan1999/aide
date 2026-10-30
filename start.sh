#!/usr/bin/env bash
# aide Linux 启动入口（等价于 macOS 的 start.command）。
# 注意：compose.macos-root.yaml 仅适用于 macOS（绑定 /Users /Volumes /private），
# Linux 不加载该覆盖文件；默认 /local 绑定到 $HOME，如需访问更多路径请自定义 compose 覆盖。
set -euo pipefail
cd "$(dirname "$0")"
export COMPOSE_FILE="${COMPOSE_FILE:-compose.yaml}"
exec bash scripts/aide.sh start
