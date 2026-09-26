#!/usr/bin/env bash
# prebuild-wheels.sh —— 在联网机上预下载 Office 文档解析依赖的 wheel 到 docker/wheels/，
# 供离线/air-gap 首次 Docker 构建使用（--build-arg PIP_OFFLINE=1）。
#
# 为什么用容器下载：宿主可能是 macOS，pip 默认会下 macOS wheel；而运行时镜像是
# linux/arm64。这里在已构建的 aide:local（即 runtime 工具链，含 python3.12）容器内下载，
# 确保拿到 manylinux_arm64 wheel，离线构建时 --no-index --find-links=/wheels 可直接装。
# 需先有 aide:local 镜像（bash scripts/aide.sh start 构建一次即可）。
#
# 用法：bash scripts/prebuild-wheels.sh
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"

DOCKER_BIN="$(command -v docker || true)"
if [[ -z "$DOCKER_BIN" && -x "$HOME/.docker/bin/docker" ]]; then DOCKER_BIN="$HOME/.docker/bin/docker"; fi
if [[ -z "$DOCKER_BIN" ]]; then echo "请先安装 Docker。" >&2; exit 1; fi
if ! "$DOCKER_BIN" image inspect aide:local >/dev/null 2>&1; then
  echo "需要先构建 aide:local 镜像（bash scripts/aide.sh start 一次）。" >&2; exit 1
fi

# 与 Dockerfile 中 pin 的版本保持一致；改动请同步 Dockerfile 的 PIP 安装行
DEPS="python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4"
# 下载用的索引（联网机可达即可；最终离线安装走 --no-index，与索引无关）
PIP_INDEX_URL="${PIP_INDEX_URL:-https://mirrors.aliyun.com/pypi/simple/}"

mkdir -p docker/wheels
# 清掉旧 wheel（保留 .gitkeep），避免残留旧版本干扰 --no-index 解析
find docker/wheels -type f ! -name .gitkeep -delete

echo "== 在 aide:local 容器内下载 linux/arm64 wheel 到 docker/wheels/（index=${PIP_INDEX_URL}）=="
"$DOCKER_BIN" run --rm -v "$PROJECT_DIR/docker/wheels:/wheels" --entrypoint /usr/local/bin/python aide:local \
  -m pip download -i "$PIP_INDEX_URL" -d /wheels $DEPS

echo "== 已下载： =="
ls -1 docker/wheels/ | grep -v '^\.gitkeep$' || true
echo
echo "下一步：把 docker/wheels/ 随源码带到离线构建机，构建时加 --build-arg PIP_OFFLINE=1"
