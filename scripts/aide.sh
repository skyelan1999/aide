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

# source_sha：计算影响二进制的构建输入哈希（cmd+internal+go.mod/go.sum/vendor + Dockerfile/compose.yaml）。
# 与 Dockerfile 的 COPY 输入一致；plugins/ 运行时加载、不编入二进制，不纳入。
# 哈希写进镜像 label aide.srcsha，供 start 二次启动时比对，命中即跳过构建。
source_sha() {
  (
    find cmd internal vendor -type f -print0 2>/dev/null
    printf '%s\0' go.mod go.sum Dockerfile compose.yaml
  ) | sort -z | xargs -0 shasum -a 256 2>/dev/null | shasum -a 256 | awk '{print $1}'
}

case "${1:-start}" in
  start|start-image)
    if [[ "${1:-start}" == start-image ]]; then
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
      IMG="${AIDE_IMAGE:-aide:local}"
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
    if [[ "$(uname -s)" == Darwin ]]; then open "$LOGIN_URL";
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
    (cd docker-images && shasum -a 256 aide-local.tar.gz > aide-local.tar.gz.sha256)
    echo "镜像已导出：$ARCHIVE"
    ;;
  load) "$DOCKER_BIN" image load -i "$PROJECT_DIR/docker-images/aide-local.tar.gz" ;;
  *) echo "用法：scripts/aide.sh {start|start-image|stop|status|logs|test|export|load}" >&2; exit 1 ;;
esac
