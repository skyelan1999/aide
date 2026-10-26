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
DEPS=(python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4 'pypdf[crypto]==6.19.0')
# 下载用的索引（联网机可达即可；最终离线安装走 --no-index，与索引无关）
PIP_INDEX_URL="${PIP_INDEX_URL:-https://mirrors.aliyun.com/pypi/simple/}"

mkdir -p docker
if [[ -L docker/wheels || ( -e docker/wheels && ! -d docker/wheels ) ]]; then
  echo "docker/wheels 必须是普通目录，已保留现有路径。" >&2; exit 1
fi
# 同一文件系统内暂存；并发脚本不相互覆盖，不抢占已有锁。
WHEEL_LOCK="$PROJECT_DIR/docker/.wheelhouse-lock"
if ! mkdir "$WHEEL_LOCK"; then
  echo "另一次 wheel 预下载持有 $WHEEL_LOCK；请等待，异常退出遗留的锁需先人工检查。" >&2; exit 1
fi
WHEEL_STAGE=""
WHEEL_PROMOTED=0
cleanup_wheels() {
  local status=$? keep_stage=0
  if [[ -n "$WHEEL_STAGE" && -d "$WHEEL_STAGE/previous" && "$WHEEL_PROMOTED" != 1 ]]; then
    if [[ ! -e docker/wheels && ! -L docker/wheels ]] && mv "$WHEEL_STAGE/previous" docker/wheels; then
      echo "替换未完成，已恢复原 wheel 目录。" >&2
    else
      echo "原 wheel 保存在 $WHEEL_STAGE/previous；未覆盖现有路径，请人工恢复。" >&2
      keep_stage=1; status=1
    fi
  fi
  if [[ -n "$WHEEL_STAGE" && "$keep_stage" == 0 ]]; then rm -rf -- "$WHEEL_STAGE"; fi
  rmdir "$WHEEL_LOCK" || status=1
  exit "$status"
}
trap cleanup_wheels EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
WHEEL_STAGE="$(mktemp -d "$PROJECT_DIR/docker/.wheelhouse-stage.XXXXXX")"
WHEEL_OWNER="$(id -u):$(id -g)"
mkdir "$WHEEL_STAGE/next"

echo "== 在 aide:local 容器内下载到隔离暂存目录 =="
"$DOCKER_BIN" run --rm --user "$WHEEL_OWNER" -v "$WHEEL_STAGE/next:/wheels" --entrypoint /usr/local/bin/python aide:local \
  -m pip download --only-binary=:all: -i "$PIP_INDEX_URL" -d /wheels "${DEPS[@]}"
# 忽略容器里已装的包，证明固定依赖及完整传递依赖能仅凭新目录解析。
"$DOCKER_BIN" run --rm --user "$WHEEL_OWNER" -v "$WHEEL_STAGE/next:/wheels:ro" --entrypoint /usr/local/bin/python aide:local \
  -m pip install --dry-run --ignore-installed --only-binary=:all: --no-index --find-links=/wheels "${DEPS[@]}"

if [[ -f docker/wheels/.gitkeep ]]; then cp -p docker/wheels/.gitkeep "$WHEEL_STAGE/next/.gitkeep"; fi
if [[ -d docker/wheels ]]; then mv docker/wheels "$WHEEL_STAGE/previous"; fi
mv "$WHEEL_STAGE/next" docker/wheels
WHEEL_PROMOTED=1

echo "== 已下载： =="
ls -1 docker/wheels/ | grep -v '^\.gitkeep$' || true
echo
echo "下一步：把 docker/wheels/ 随源码带到离线构建机，构建时加 --build-arg PIP_OFFLINE=1"
