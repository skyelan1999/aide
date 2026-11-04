#!/usr/bin/env bash
# prebuild-wheels.sh —— 在联网机上按明确的 Linux 目标平台下载 Office 依赖，
# 供离线/air-gap Docker 构建使用（--build-arg PIP_OFFLINE=1）。
#
# 为什么用容器下载：宿主可能是 macOS，pip 默认会下 macOS wheel；而运行时镜像是
# Linux。这里使用 aide:local 中的 Python 3.12，但总是向 pip 显式指定目标操作系统、架构、
# Python 实现和 ABI；aide:local 本身的架构不影响生成的目标 wheelhouse。
# 需先有 aide:local 镜像（bash scripts/aide.sh start 构建一次即可）。
#
# 用法：bash scripts/prebuild-wheels.sh [--target-platform linux/arm64|linux/amd64]
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"

TARGET_PLATFORM="${AIDE_WHEEL_TARGET_PLATFORM:-linux/arm64}"
if [[ $# -gt 0 ]]; then
  if [[ $# -ne 2 || "$1" != "--target-platform" ]]; then
    echo "用法：$0 [--target-platform linux/arm64|linux/amd64]" >&2; exit 2
  fi
  TARGET_PLATFORM="$2"
fi
case "$TARGET_PLATFORM" in
  linux/arm64)
    PIP_TARGET=(--platform manylinux_2_28_aarch64 --platform manylinux_2_17_aarch64 --platform manylinux2014_aarch64 --platform linux_aarch64 --platform any)
    ;;
  linux/amd64)
    PIP_TARGET=(--platform manylinux_2_28_x86_64 --platform manylinux_2_17_x86_64 --platform manylinux2014_x86_64 --platform linux_x86_64 --platform any)
    ;;
  *) echo "不支持 wheelhouse 目标：$TARGET_PLATFORM（仅支持 linux/arm64 与 linux/amd64）。" >&2; exit 2 ;;
esac
PIP_TARGET+=(--python-version 3.12 --implementation cp --abi cp312 --abi abi3 --abi none)

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

echo "== 为 ${TARGET_PLATFORM} / CPython 3.12 下载到隔离暂存目录 =="
"$DOCKER_BIN" run --rm --user "$WHEEL_OWNER" -v "$WHEEL_STAGE/next:/wheels" --entrypoint /usr/local/bin/python aide:local \
  -m pip download --only-binary=:all: "${PIP_TARGET[@]}" -i "$PIP_INDEX_URL" -d /wheels "${DEPS[@]}"
# 忽略容器里已装的包，证明固定依赖及完整传递依赖能仅凭新目录解析。
"$DOCKER_BIN" run --rm --user "$WHEEL_OWNER" -v "$WHEEL_STAGE/next:/wheels:ro" --entrypoint /usr/local/bin/python aide:local \
  -m pip install --dry-run --ignore-installed --only-binary=:all: "${PIP_TARGET[@]}" --no-index --find-links=/wheels "${DEPS[@]}"

# Offline Docker builds check this marker against TARGETOS/TARGETARCH and the
# pinned Python before pip is allowed to consume a wheelhouse.
printf 'platform=%s\npython=3.12\nimplementation=cp\nabi=cp312\n' "$TARGET_PLATFORM" > "$WHEEL_STAGE/next/.aide-wheelhouse-target"

if [[ -f docker/wheels/.gitkeep ]]; then cp -p docker/wheels/.gitkeep "$WHEEL_STAGE/next/.gitkeep"; fi
if [[ -d docker/wheels ]]; then mv docker/wheels "$WHEEL_STAGE/previous"; fi
mv "$WHEEL_STAGE/next" docker/wheels
WHEEL_PROMOTED=1

echo "== 已下载： =="
ls -1 docker/wheels/ | grep -v '^\.gitkeep$' || true
echo
echo "下一步：把 docker/wheels/ 随源码带到离线构建机；仅用于 ${TARGET_PLATFORM}、Python 3.12 的目标镜像，并加 --build-arg PIP_OFFLINE=1。"
