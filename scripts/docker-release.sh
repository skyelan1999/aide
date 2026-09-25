#!/usr/bin/env bash
# docker-release.sh —— 联网构建机上执行的离线镜像发布脚本。
#
# 目标：在有网络的机器上一次性构建出自包含、可离线运行的 aide 镜像，
#       产物为 镜像 tar.gz + 统一 SHA256SUMS 清单。
#       目标离线机只做 docker load + start-image，绝不再 pull / build。
#
# 用法：bash scripts/docker-release.sh
# 前置：本机可访问 Docker 引擎、可联网拉取基础镜像；在 Git 仓库干净工作树上执行。
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"

# 复用 scripts/version.sh 的版本规则，不另造版本号（如 0.1.10.2 RC1）
VER_RAW="$(bash scripts/version.sh show)"
# 文件名/标签不允许空格：0.1.10.2 RC1 -> 0.1.10.2-RC1
VER="$(echo "$VER_RAW" | sed 's/ /-/g')"
# Docker 内部架构 arm64；归档命名统一用 aarch64/amd64
ARCH="$(uname -m | sed 's/arm64/aarch64/;s/x86_64/amd64/')"
COMMIT="$(git rev-parse HEAD)"
TAG="aide:${VER}"

# 与 aide.sh 一致地发现 docker 可执行文件
DOCKER_BIN="$(command -v docker || true)"
if [[ -z "$DOCKER_BIN" && -x "$HOME/.docker/bin/docker" ]]; then
  DOCKER_BIN="$HOME/.docker/bin/docker"
fi
if [[ -z "$DOCKER_BIN" ]]; then
  echo "请先安装 Docker Desktop / Docker Engine。" >&2
  exit 1
fi

echo "== 发布参数 =="
echo "  VER_RAW = $VER_RAW"
echo "  VER     = $VER"
echo "  ARCH    = $ARCH"
echo "  COMMIT  = $COMMIT"
echo "  TAG     = $TAG"

# 1) 构建（注入版本/commit；基础镜像在联网阶段拉取并固化进产物）
echo "== [1/4] docker build =="
"$DOCKER_BIN" build \
  --build-arg AIDE_VERSION="$VER_RAW" \
  --build-arg AIDE_COMMIT="$COMMIT" \
  -t "$TAG" .

# 2) 冒烟测试：容器内 healthz 通过才算可交付
echo "== [2/4] 冒烟测试 healthz =="
TEST_NAME="aide-rel-test"
cleanup() { "$DOCKER_BIN" rm -f "$TEST_NAME" >/dev/null 2>&1 || true; }
"$DOCKER_BIN" rm -f "$TEST_NAME" >/dev/null 2>&1 || true
trap cleanup EXIT
"$DOCKER_BIN" run --rm -d --name "$TEST_NAME" -p 127.0.0.1:8099:8080 "$TAG"

ok=0
for attempt in {1..30}; do
  if curl -sf http://127.0.0.1:8099/healthz >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
"$DOCKER_BIN" stop "$TEST_NAME" >/dev/null
if [[ "$ok" -ne 1 ]]; then
  echo "冒烟测试失败：healthz 未通过，已中止发布。" >&2
  exit 1
fi
echo "  healthz PASS"

# 3) save + gzip（先写 .partial，成功后原子替换，避免留下残缺归档）
echo "== [3/4] docker save + gzip =="
mkdir -p docker-images
OUT="docker-images/aide-${VER}-linux-${ARCH}.tar.gz"
"$DOCKER_BIN" save "$TAG" | gzip -1 > "${OUT}.partial"
mv "${OUT}.partial" "$OUT"
echo "  产物：$OUT ($(du -h "$OUT" | cut -f1))"

# 4) 生成统一 SHA256SUMS 清单（单文件、相对路径）
#    历史零散 *.sha256 废弃；install.sh 与离线部署统一校验本文件。
echo "== [4/4] SHA256SUMS =="
(cd docker-images && shasum -a 256 "aide-${VER}-linux-${ARCH}.tar.gz" > SHA256SUMS)
cat docker-images/SHA256SUMS

echo "== 完成 =="
echo "交付物三件套：同 tag 源码 + $OUT + docker-images/SHA256SUMS"
echo "目标离线机：shasum -a 256 -c SHA256SUMS && docker load -i <归档> && bash scripts/aide.sh start-image"
