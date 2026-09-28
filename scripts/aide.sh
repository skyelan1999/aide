#!/usr/bin/env bash
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"
DOCKER_BIN="$(command -v docker || true)"
if [[ -z "$DOCKER_BIN" && -x "$HOME/.docker/bin/docker" ]]; then DOCKER_BIN="$HOME/.docker/bin/docker"; fi
if [[ -z "$DOCKER_BIN" ]]; then echo "请先安装 Docker Desktop。" >&2; exit 1; fi
if ! "$DOCKER_BIN" info >/dev/null 2>&1; then
  if [[ "$(uname -s)" == Darwin ]]; then open -a Docker; fi
  echo "等待 Docker 引擎…"
  for attempt in {1..60}; do if "$DOCKER_BIN" info >/dev/null 2>&1; then break; fi; sleep 1; done
  "$DOCKER_BIN" info >/dev/null
fi

# source_sha：覆盖应用源码和 Dockerfile COPY 的 Office、wheel、语音资源。
# 哈希写进镜像 label aide.srcsha，供 start 二次启动时比对，命中即跳过构建。
source_sha() {
  (
    find cmd internal vendor scripts/office docker/wheels docker/sherpa -type f -print0
    printf '%s\0' go.mod go.sum Dockerfile compose.yaml .dockerignore version.md
  ) | sort -z | xargs -0 shasum -a 256 2>/dev/null | shasum -a 256 | awk '{print $1}'
}

MODE="${1:-start}"
if [[ -f .aide-image ]]; then
  read -r BUNDLE_IMAGE BUNDLE_ID BUNDLE_PLATFORM < .aide-image
  export AIDE_IMAGE="$BUNDLE_IMAGE" COMPOSE_FILE=compose.yaml
fi
if [[ "$MODE" == start-bundle ]]; then
  [[ -f .aide-image ]] || { echo "当前目录不是离线包：缺少 .aide-image。" >&2; exit 1; }
  ENGINE_PLATFORM="$("$DOCKER_BIN" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|')"
  if [[ "$ENGINE_PLATFORM" != "$BUNDLE_PLATFORM" ]]; then
    echo "镜像架构 $BUNDLE_PLATFORM 与 Docker 引擎 $ENGINE_PLATFORM 不匹配，请使用对应架构的包。" >&2
    exit 1
  fi
  CURRENT_ID="$("$DOCKER_BIN" image inspect "$BUNDLE_IMAGE" --format '{{.Id}}' 2>/dev/null || true)"
  if [[ "$CURRENT_ID" != "$BUNDLE_ID" ]]; then
    echo "== 校验并导入包内镜像 =="
    (cd docker-images && shasum -a 256 -c SHA256SUMS)
    "$DOCKER_BIN" image load -i docker-images/aide-local.tar
  fi
  [[ "$("$DOCKER_BIN" image inspect "$BUNDLE_IMAGE" --format '{{.Id}}')" == "$BUNDLE_ID" ]] || { echo "镜像身份不匹配。" >&2; exit 1; }
  mkdir -p workspace context
  if [[ ! -e .env ]]; then cp .env.example .env; fi
  export AIDE_IMAGE="$BUNDLE_IMAGE" COMPOSE_FILE=compose.yaml
  MODE=start-image
fi

case "$MODE" in
  start|start-image)
    if [[ "$MODE" == start-image ]]; then
      "$DOCKER_BIN" compose up -d --no-build --pull never
    else
      export AIDE_VERSION="${AIDE_VERSION:-$(bash scripts/version.sh show)}"
      if [[ -z "${AIDE_COMMIT:-}" ]]; then
        AIDE_COMMIT="$(git rev-parse HEAD)"
        if [[ -n "$(git status --porcelain)" ]]; then AIDE_COMMIT="${AIDE_COMMIT}-dirty"; fi
        export AIDE_COMMIT
      fi
      # #43 快捷路径：比对镜像 aide.srcsha label 与当前源码哈希。
      # 命中（镜像存在且构建输入未变）→ --no-build 直起，跳过 buildkit 上下文发送与层检查；
      # 未命中 → --build（默认不跑全量 test；层缓存命中时仅 vet+增量 build，秒级）。
      SRCSHA="$(source_sha)"
      # Compose resolves shell variables and .env; never execute .env as shell code.
      IMG="$("$DOCKER_BIN" compose config --images aide)"
      cur_label=""
      if "$DOCKER_BIN" image inspect "$IMG" >/dev/null 2>&1; then
        cur_label="$("$DOCKER_BIN" image inspect "$IMG" --format '{{ index .Config.Labels "aide.srcsha" }}' 2>/dev/null || true)"
      fi
      if [[ -n "$SRCSHA" && "$cur_label" == "$SRCSHA" ]]; then
        echo "== 源码哈希未变（${SRCSHA}）→ 快捷路径：跳过构建，直接启动 =="
        "$DOCKER_BIN" compose up -d --no-build --pull never
      else
        echo "== 源码哈希变化（image=${cur_label:-<无>} → worktree=${SRCSHA}）→ 重新构建（默认不跑全量 test，仅 vet+build）=="
        export AIDE_SRC_SHA="$SRCSHA"
        "$DOCKER_BIN" compose up -d --build --pull never
      fi
    fi
    for attempt in {1..60}; do if "$DOCKER_BIN" compose exec -T aide curl -fsSk https://127.0.0.1:8080/healthz >/dev/null 2>&1; then break; fi; sleep 1; done
    "$DOCKER_BIN" compose exec -T aide curl -fsSk https://127.0.0.1:8080/healthz >/dev/null
    ADDRESS="$("$DOCKER_BIN" compose port aide 8080)"
    HOSTPORT="${ADDRESS##*:}"
    # #31 数据分层后令牌位于 /data/auth/access-token；兼容旧路径，读取失败不中断启动
    TOKEN="$("$DOCKER_BIN" compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' || true)"
    if [[ -n "$TOKEN" ]]; then LOGIN_URL="https://localhost:$HOSTPORT/#token=$TOKEN"; else LOGIN_URL="https://localhost:$HOSTPORT/"; fi
    echo "aide 已就绪：https://$ADDRESS （自签证书，浏览器告警请选 继续/高级→仍要访问）"
    if [[ -z "$TOKEN" ]]; then echo "未自动读到令牌，已打开登录页；可运行：$DOCKER_BIN compose exec -T aide cat /data/auth/access-token"; fi
    if [[ "${AIDE_OPEN_BROWSER:-1}" == 0 ]]; then :;
    elif [[ "$(uname -s)" == Darwin ]]; then open "$LOGIN_URL";
    elif command -v xdg-open >/dev/null; then xdg-open "$LOGIN_URL";
    else echo "在浏览器打开 $LOGIN_URL ；令牌位于容器 /data/auth/access-token，自签证书浏览器会告警，选择继续。"; fi
    ;;
  stop) "$DOCKER_BIN" compose stop ;;
  status) "$DOCKER_BIN" compose ps ;;
  logs) "$DOCKER_BIN" compose logs --tail=100 aide ;;
  test) "$DOCKER_BIN" run --rm -v "$PROJECT_DIR:/src" -w /src --entrypoint bash aide:local -c 'go test -race -count=1 ./... && go vet ./...' ;;
  export)
    mkdir -p docker-images
    ARCHIVE="$PROJECT_DIR/docker-images/aide-local.tar.gz"
    trap 'rm -f "$ARCHIVE.partial"' EXIT
    "$DOCKER_BIN" image save aide:local | gzip -1 > "$ARCHIVE.partial"
    mv "$ARCHIVE.partial" "$ARCHIVE"
    (cd docker-images && shasum -a 256 aide-local.tar.gz > SHA256SUMS)
    echo "镜像已导出：$ARCHIVE"
    ;;
  export-bundle)
    # Package the already-built image. Never include .env, host files or data volumes.
    IMG="$("$DOCKER_BIN" compose config --images aide)"
    if [[ "$("$DOCKER_BIN" image inspect "$IMG" --format '{{index .Config.Labels "aide.srcsha"}}')" != "$(source_sha)" ]]; then
      echo "镜像与当前构建输入不一致，请先运行 start.command 完成构建。" >&2
      exit 1
    fi
    IMAGE_ID="$("$DOCKER_BIN" image inspect "$IMG" --format '{{.Id}}')"
    PLATFORM="$("$DOCKER_BIN" image inspect "$IMG" --format '{{.Os}}/{{.Architecture}}')"
    SHORT_ID="${IMAGE_ID#sha256:}"
    SHORT_ID="${SHORT_ID:0:12}"
    BUNDLE_TAG="aide:offline-$SHORT_ID"
    VERSION="$(bash scripts/version.sh show | tr ' ' '-')"
    mkdir -p docker-images .agent-state
    ARCHIVE="$PROJECT_DIR/docker-images/aide-$VERSION-${PLATFORM//\//-}-$SHORT_ID-offline.tar.gz"
    STAGING="$(mktemp -d "$PROJECT_DIR/.agent-state/bundle.XXXXXX")"
    trap 'rm -rf "$STAGING"; rm -f "$ARCHIVE.partial"' EXIT
    BUNDLE="$STAGING/aide-offline"
    mkdir -p "$BUNDLE/scripts" "$BUNDLE/docker-images" "$BUNDLE/workspace" "$BUNDLE/context"
    cp start.command "$BUNDLE/"
    cp scripts/aide.sh "$BUNDLE/scripts/"
    cp docker/compose.offline.yaml "$BUNDLE/compose.yaml"
    cp docker/offline.env.example "$BUNDLE/.env.example"
    cp docker/OFFLINE.md "$BUNDLE/README.md"
    cp LICENSE "$BUNDLE/"
    printf '%s %s %s\n' "$BUNDLE_TAG" "$IMAGE_ID" "$PLATFORM" > "$BUNDLE/.aide-image"
    printf 'version=%s\nimage=%s\nplatform=%s\nsrcsha=%s\n' "$VERSION" "$IMAGE_ID" "$PLATFORM" \
      "$("$DOCKER_BIN" image inspect "$IMG" --format '{{index .Config.Labels "aide.srcsha"}}')" > "$BUNDLE/BUILD.txt"
    "$DOCKER_BIN" image tag "$IMAGE_ID" "$BUNDLE_TAG"
    "$DOCKER_BIN" image save -o "$BUNDLE/docker-images/aide-local.tar" "$BUNDLE_TAG"
    (cd "$BUNDLE/docker-images" && shasum -a 256 aide-local.tar > SHA256SUMS)
    COPYFILE_DISABLE=1 tar -czf "$ARCHIVE.partial" -C "$STAGING" aide-offline
    mv "$ARCHIVE.partial" "$ARCHIVE"
    (cd docker-images && shasum -a 256 "$(basename "$ARCHIVE")" > SHA256SUMS)
    echo "离线启动包已导出：$ARCHIVE"
    ;;
  load) "$DOCKER_BIN" image load -i "$PROJECT_DIR/docker-images/aide-local.tar.gz" ;;
  *) echo "用法：scripts/aide.sh {start|start-image|start-bundle|stop|status|logs|test|export|export-bundle|load}" >&2; exit 1 ;;
esac
