#!/usr/bin/env bash
set -euo pipefail
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"
MODE="${1:-start}"
# source_sha：覆盖应用与构建资源；排除启动时重写的插件能力快照。
# 哈希写进镜像 label aide.srcsha，供 start 二次启动时比对，命中即跳过构建。
source_sha() {
  (
    find cmd internal plugins vendor scripts/office scripts/cad docker/cad docker/wheels docker/sherpa -type d -name node_modules -prune -o -type f ! -name '.DS_Store' ! -name '*.pyc' ! -path '*/__pycache__/*' ! -path 'plugins/surface.json' -print0
    printf '%s\0' go.mod go.sum Dockerfile compose.yaml .dockerignore version.md scripts/aide.sh scripts/launcher-data.sh scripts/launcher-data.ps1 scripts/launcher-data.py
    for extra in Dockerfile.local docker/Dockerfile.local-base scripts/prepare-local-base.sh scripts/prepare-cad-base.sh scripts/prebuilt-image.sh scripts/prebuilt-image.ps1 scripts/resource-guard.sh scripts/resource-guard.ps1 docker/Dockerfile.cad start.command start.sh start.ps1 start.bat docker/compose.offline.yaml docker/offline.env.example scripts/build-message-lan.sh scripts/message-lan-control.sh scripts/message-lan-control.ps1; do
      if [[ -f "$extra" ]]; then printf '%s\0' "$extra"; fi
    done
  ) | LC_ALL=C sort -z | xargs -0 shasum -a 256 2>/dev/null | shasum -a 256 | awk '{print $1}'
}
record_archive_sum() {
  local archive_name checksum temporary
  archive_name="$(basename "$1")"
  checksum="$(shasum -a 256 "$1" | awk '{print $1}')"
  temporary="$(mktemp docker-images/checksums.XXXXXX)"
  if [[ -f docker-images/SHA256SUMS ]]; then awk -v name="$archive_name" '$2 != name' docker-images/SHA256SUMS > "$temporary"; fi
  printf '%s  %s\n' "$checksum" "$archive_name" >> "$temporary"
  mv "$temporary" docker-images/SHA256SUMS
}
record_prebuilt_image() {
  local platform="$1" image="$2" source="$3" archive="$4" tag="$5" checksum temporary
  local candidate=".agent-state/prebuilt-images.candidate.tsv"
  checksum="$(shasum -a 256 "$archive" | awk '{print $1}')"
  mkdir -p .agent-state
  temporary="$(mktemp .agent-state/prebuilt-images.XXXXXX)"
  if [[ -f "$candidate" ]]; then awk -v p="$platform" '$1!=p' "$candidate" > "$temporary";
  else printf '# Local candidates only; verify public Release assets before updating docker/prebuilt-images.tsv.\n' > "$temporary"; fi
  printf '%s %s %s %s https://github.com/skyelan1999/aide/releases/download/%s/%s bundle\n' "$platform" "$image" "$source" "$checksum" "$tag" "$(basename "$archive")" >> "$temporary"
  mv "$temporary" "$candidate"
  echo "候选镜像清单：${candidate}；公开附件验证通过后再更新默认下载清单。"
}
if [[ "$MODE" == source-sha ]]; then source_sha; exit; fi
if [[ "$MODE" == stop || "$MODE" == clean-cache ]]; then
  exec bash scripts/aide-maintenance.sh "$@"
fi
if [[ "$MODE" == test && "${AIDE_VERIFY_IN_CONTAINER:-0}" == 1 ]]; then
  # The caller already launched an isolated aide:local verification container.
  export AIDE_BUILTIN_PLUGINS=
  go test -race -count=1 -timeout=15m ./... && go vet ./...
  exit
fi
if [[ "$MODE" == start || "$MODE" == start-image || "$MODE" == start-bundle ]]; then
  source scripts/resource-guard.sh
  aide_check_disk start
  source scripts/launcher-data.sh
  launcher_env_restore
  if [[ "$MODE" == start && -f .aide-image ]]; then MODE=start-bundle; fi
fi
DOCKER_BIN="$(command -v docker || true)"
if [[ -z "$DOCKER_BIN" && -x "$HOME/.docker/bin/docker" ]]; then DOCKER_BIN="$HOME/.docker/bin/docker"; fi
if [[ -z "$DOCKER_BIN" ]]; then echo "请先安装 Docker Desktop。" >&2; exit 1; fi
if ! "$DOCKER_BIN" info >/dev/null 2>&1; then
  if [[ "$(uname -s)" == Darwin ]]; then open -a Docker; fi
  echo "等待 Docker 引擎…"
  for attempt in {1..60}; do if "$DOCKER_BIN" info >/dev/null 2>&1; then break; fi; sleep 1; done
  "$DOCKER_BIN" info >/dev/null
fi

if [[ "$MODE" == start || "$MODE" == start-image || "$MODE" == start-bundle ]]; then
  aide_check_engine_resources
fi

# A/B restarts must address the Compose project that owns the already-running
# bundle. The launcher can be started from Finder or another shell, so inherit
# its project name from the container label instead of relying on caller env.
if [[ -f .aide-image ]]; then
  RUNNING_BUNDLE="$("$DOCKER_BIN" ps -q --filter "label=com.docker.compose.project.working_dir=$PROJECT_DIR" --filter label=com.docker.compose.service=aide | head -1)"
  if [[ -n "$RUNNING_BUNDLE" ]]; then
    RUNNING_PROJECT="$("$DOCKER_BIN" inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$RUNNING_BUNDLE" 2>/dev/null || true)"
    if [[ -n "$RUNNING_PROJECT" ]]; then export COMPOSE_PROJECT_NAME="$RUNNING_PROJECT"; fi
  fi
fi



if [[ -f .aide-image ]]; then
  read -r BUNDLE_IMAGE BUNDLE_ID BUNDLE_PLATFORM BUNDLE_RELEASE_TAG < .aide-image
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
    IMAGE_ARCHIVE="docker-images/aide-local.tar"
    [[ -f "$IMAGE_ARCHIVE" ]] || IMAGE_ARCHIVE="docker-images/aide-local.tar.gz"
    if [[ ! -f "$IMAGE_ARCHIVE" ]]; then
      IMAGE_ARCHIVE="$(find docker-images -maxdepth 1 -type f -name 'aide-v*-linux-*-image.tar.gz' -print -quit)"
    fi
    if [[ -n "${BUNDLE_RELEASE_TAG:-}" ]]; then
      RELEASE_IMAGE="docker-images/aide-${BUNDLE_RELEASE_TAG}-linux-${BUNDLE_PLATFORM#linux/}-image.tar.gz"
      [[ -f "$IMAGE_ARCHIVE" ]] || IMAGE_ARCHIVE="$RELEASE_IMAGE"
      IMAGE_URL="https://github.com/skyelan1999/aide/releases/download/${BUNDLE_RELEASE_TAG}/${RELEASE_IMAGE##*/}"
      SUMS_URL="https://github.com/skyelan1999/aide/releases/download/${BUNDLE_RELEASE_TAG}/SHA256SUMS"
    fi
    if [[ ! -f "$IMAGE_ARCHIVE" ]]; then
      [[ -n "${BUNDLE_RELEASE_TAG:-}" ]] || { echo "包内缺少镜像，且 .aide-image 没有 Release tag，无法自动下载。" >&2; exit 1; }
      command -v curl >/dev/null 2>&1 || { echo "缺少 curl，无法自动下载镜像。请安装 curl 后重试。" >&2; exit 1; }
      echo "包内没有镜像，正在从 GitHub Release 下载（约 500 MB）：$BUNDLE_RELEASE_TAG"
    fi
    if [[ ! -f docker-images/SHA256SUMS && -n "${BUNDLE_RELEASE_TAG:-}" ]]; then
      command -v curl >/dev/null 2>&1 || { echo "缺少 curl，无法下载 SHA256SUMS。" >&2; exit 1; }
      curl --fail --location --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 30 --output docker-images/SHA256SUMS.partial "$SUMS_URL"
      mv docker-images/SHA256SUMS.partial docker-images/SHA256SUMS
    fi
    if [[ ! -f "$IMAGE_ARCHIVE" && -n "${BUNDLE_RELEASE_TAG:-}" ]]; then
      # GitHub/CDN connections can be closed mid-transfer. Keep the partial
      # file and resume it on the next attempt; only promote a complete file
      # after its digest matches the published checksum.
      ARCHIVE_NAME="$(basename "$IMAGE_ARCHIVE")"
      EXPECTED_HASH="$(awk -v name="$ARCHIVE_NAME" '{file=$2; sub(/^\*/, "", file); if (file == name) {print $1; exit}}' docker-images/SHA256SUMS 2>/dev/null || true)"
      [[ "$EXPECTED_HASH" =~ ^[[:xdigit:]]{64}$ ]] || { echo "SHA256SUMS 中没有 $ARCHIVE_NAME 的有效校验值。" >&2; exit 1; }
      downloaded=0
      for attempt in {1..8}; do
        if curl --fail --location --retry 3 --retry-all-errors --retry-delay 2 --connect-timeout 30 --continue-at - --output "$IMAGE_ARCHIVE.partial" "$IMAGE_URL"; then
          if command -v shasum >/dev/null 2>&1; then ACTUAL_HASH="$(shasum -a 256 "$IMAGE_ARCHIVE.partial" | awk '{print $1}')";
          elif command -v sha256sum >/dev/null 2>&1; then ACTUAL_HASH="$(sha256sum "$IMAGE_ARCHIVE.partial" | awk '{print $1}')";
          else echo "缺少 shasum/sha256sum，无法校验下载镜像。" >&2; exit 1; fi
          if [[ "$ACTUAL_HASH" == "$EXPECTED_HASH" ]]; then
            mv "$IMAGE_ARCHIVE.partial" "$IMAGE_ARCHIVE"
            downloaded=1
            break
          fi
          echo "下载内容校验未通过，重新下载（尝试 $attempt/8）…" >&2
          rm -f "$IMAGE_ARCHIVE.partial"
        else
          echo "镜像下载中断，正在从已下载位置续传（尝试 $attempt/8）…" >&2
        fi
      done
      if (( downloaded == 0 )); then
        echo "镜像下载未完成。请检查网络后重新运行 start.command；已下载部分会尝试续传。" >&2
        exit 1
      fi
    fi
    [[ -f "$IMAGE_ARCHIVE" ]] || { echo "缺少镜像归档：docker-images/aide-local.tar[.gz]。" >&2; exit 1; }
    ARCHIVE_NAME="$(basename "$IMAGE_ARCHIVE")"
    EXPECTED_HASH="$(awk -v name="$ARCHIVE_NAME" '{file=$2; sub(/^\*/, "", file); if (file == name) {print $1; exit}}' docker-images/SHA256SUMS 2>/dev/null || true)"
    [[ "$EXPECTED_HASH" =~ ^[[:xdigit:]]{64}$ ]] || { echo "SHA256SUMS 中没有 $ARCHIVE_NAME 的有效校验值。" >&2; exit 1; }
    if command -v shasum >/dev/null 2>&1; then ACTUAL_HASH="$(shasum -a 256 "$IMAGE_ARCHIVE" | awk '{print $1}')";
    elif command -v sha256sum >/dev/null 2>&1; then ACTUAL_HASH="$(sha256sum "$IMAGE_ARCHIVE" | awk '{print $1}')";
    else echo "缺少 shasum/sha256sum，无法校验下载镜像。" >&2; exit 1; fi
    [[ "$ACTUAL_HASH" == "$EXPECTED_HASH" ]] || { echo "Docker 镜像 SHA256 校验失败。" >&2; rm -f "$IMAGE_ARCHIVE"; exit 1; }
    "$DOCKER_BIN" image load -i "$IMAGE_ARCHIVE"
  fi
  # Release launchers address images through stable A/B tags. Migrate old
  # bundles after loading the immutable release tag, without touching data.
  if [[ "$BUNDLE_IMAGE" != aide:slot-* ]]; then BUNDLE_IMAGE=aide:slot-a; fi
  SLOT_ID="$("$DOCKER_BIN" image inspect "$BUNDLE_IMAGE" --format '{{.Id}}' 2>/dev/null || true)"
  if [[ "$SLOT_ID" != "$BUNDLE_ID" ]]; then "$DOCKER_BIN" image tag "$BUNDLE_ID" "$BUNDLE_IMAGE"; fi
  printf '%s %s %s %s\n' "$BUNDLE_IMAGE" "$BUNDLE_ID" "$BUNDLE_PLATFORM" "${BUNDLE_RELEASE_TAG:-}" > .aide-image
  [[ "$("$DOCKER_BIN" image inspect "$BUNDLE_IMAGE" --format '{{.Id}}')" == "$BUNDLE_ID" ]] || { echo "镜像身份不匹配。" >&2; exit 1; }
  mkdir -p workspace context
  if [[ ! -e .env ]]; then cp .env.example .env; fi
  launcher_data_prepare "$DOCKER_BIN" "$BUNDLE_IMAGE"
  SAVED_PORT="$(cat .aide-data/storage/data/config/host-port 2>/dev/null | tr -d '\r\n' || true)"
  if [[ "$SAVED_PORT" =~ ^[0-9]{1,5}$ ]] && (( SAVED_PORT >= 1 && SAVED_PORT <= 65535 )); then
    AIDE_PORT="$SAVED_PORT"
    export AIDE_PORT
    if grep -q '^AIDE_PORT=' .env; then
      sed -i.bak "s/^AIDE_PORT=.*/AIDE_PORT=$AIDE_PORT/" .env && rm -f .env.bak
    else
      printf '\nAIDE_PORT=%s\n' "$AIDE_PORT" >> .env
    fi
  fi
  # A bundle is often started while the regular developer install is already
  # using 8097. Pick and persist the next free port instead of failing after
  # Docker has created its network and volumes.
  if [[ -z "${AIDE_PORT:-}" ]]; then
    CONFIGURED_PORT="$(awk -F= '$1 == "AIDE_PORT" { gsub(/[[:space:]]/, "", $2); print $2; exit }' .env)"
    CONFIGURED_PORT="${CONFIGURED_PORT:-8097}"
    port_is_listening() {
      if command -v lsof >/dev/null 2>&1; then
        lsof -nP -iTCP:"$1" -sTCP:LISTEN -t >/dev/null 2>&1
      elif command -v ss >/dev/null 2>&1; then
        [[ -n "$(ss -H -ltn "sport = :$1" 2>/dev/null)" ]]
      elif command -v nc >/dev/null 2>&1; then
        nc -z -w 1 127.0.0.1 "$1" >/dev/null 2>&1
      else
        return 1
      fi
    }
    # During an A/B restart this port belongs to the current aide container.
    # Reuse it; only avoid occupied ports when no bundle service is running.
    BUNDLE_RUNNING=0
    if [[ -f .aide-image ]] && "$DOCKER_BIN" compose ps --status running --services 2>/dev/null | grep -qx aide; then
      BUNDLE_RUNNING=1
    fi
    if (( BUNDLE_RUNNING == 0 )) && port_is_listening "$CONFIGURED_PORT"; then
      for candidate in $(seq "$((CONFIGURED_PORT + 1))" "$((CONFIGURED_PORT + 100))"); do
        if ! port_is_listening "$candidate"; then
          AIDE_PORT="$candidate"
          export AIDE_PORT
          if grep -q '^AIDE_PORT=' .env; then
            sed -i.bak "s/^AIDE_PORT=.*/AIDE_PORT=$AIDE_PORT/" .env && rm -f .env.bak
          else
            printf '\nAIDE_PORT=%s\n' "$AIDE_PORT" >> .env
          fi
          echo "端口 $CONFIGURED_PORT 已占用，离线包改用端口 $AIDE_PORT。"
          break
        fi
      done
      [[ -n "${AIDE_PORT:-}" ]] || { echo "端口 $CONFIGURED_PORT 至 $((CONFIGURED_PORT + 100)) 均被占用，请在 .env 中设置 AIDE_PORT。" >&2; exit 1; }
    fi
  fi
  export AIDE_IMAGE="$BUNDLE_IMAGE" COMPOSE_FILE=compose.yaml
  MODE=start-image
fi

case "$MODE" in
  start|start-image)
    rm -f .cache/aide-manual-stop
    # Per-install authentication for the macOS Safari relay. .env is parsed as
    # data by Compose and is never executed as shell code.
    if [[ ! -f .env ]]; then cp .env.example .env; fi
    BRIDGE_TOKEN="$(awk -F= '$1 == "AIDE_BROWSER_BRIDGE_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' .env)"
    if [[ ${#BRIDGE_TOKEN} -lt 32 ]]; then
      BRIDGE_TOKEN="$(openssl rand -hex 32)"
      if grep -q '^AIDE_BROWSER_BRIDGE_TOKEN=' .env; then
        sed -i.bak "s/^AIDE_BROWSER_BRIDGE_TOKEN=.*/AIDE_BROWSER_BRIDGE_TOKEN=$BRIDGE_TOKEN/" .env && rm -f .env.bak
      else
        printf '\nAIDE_BROWSER_BRIDGE_TOKEN=%s\n' "$BRIDGE_TOKEN" >> .env
      fi
    fi
    export AIDE_BROWSER_BRIDGE_TOKEN="$BRIDGE_TOKEN"
    COMPUTER_BRIDGE_TOKEN="$(awk -F= '$1 == "AIDE_COMPUTER_BRIDGE_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' .env)"
    if [[ ${#COMPUTER_BRIDGE_TOKEN} -lt 32 ]]; then
      COMPUTER_BRIDGE_TOKEN="$(openssl rand -hex 32)"
      if grep -q '^AIDE_COMPUTER_BRIDGE_TOKEN=' .env; then
        sed -i.bak "s/^AIDE_COMPUTER_BRIDGE_TOKEN=.*/AIDE_COMPUTER_BRIDGE_TOKEN=$COMPUTER_BRIDGE_TOKEN/" .env && rm -f .env.bak
      else
        printf '\nAIDE_COMPUTER_BRIDGE_TOKEN=%s\n' "$COMPUTER_BRIDGE_TOKEN" >> .env
      fi
    fi
    export AIDE_COMPUTER_BRIDGE_TOKEN="$COMPUTER_BRIDGE_TOKEN"
    REUSE_RUNNING=0
    source scripts/prebuilt-image.sh
    # Normal startup prefers a complete installed native runtime. Force/source
    # requests bypass this reuse so updating code does not require deleting it.
    if [[ "$MODE" == start && "${AIDE_FORCE_REBUILD:-0}" != 1 && "${AIDE_BUILD_FROM_SOURCE:-auto}" != 1 ]]; then
      IMG="$("$DOCKER_BIN" compose config --images aide)"
      if launcher_local_runtime_ready "$DOCKER_BIN" "$IMG"; then
        echo "== 本地完整镜像可用，直接复用；更新源码请强制重编译 =="
        export AIDE_IMAGE="$IMG"
        MODE=start-image
      fi
    fi
    if [[ "$MODE" == start ]] && ! launcher_should_build_source "${AIDE_BUILD_FROM_SOURCE:-auto}" "$PROJECT_DIR"; then
      if launcher_running_ready "$DOCKER_BIN"; then
        echo "== Aide 已在运行且健康，直接打开现有界面；不下载或替换镜像 =="
        REUSE_RUNNING=1
      else
        IMG="$("$DOCKER_BIN" compose config --images aide)"
        if launcher_local_runtime_ready "$DOCKER_BIN" "$IMG"; then
          echo "== 使用本地已安装的完整运行镜像；启动无需下载，更新请使用软件升级 =="
        else
          launcher_prebuilt_image "$DOCKER_BIN" "$IMG"
        fi
        export AIDE_IMAGE="$IMG"
        MODE=start-image
      fi
    fi
    if [[ "$REUSE_RUNNING" != 1 ]]; then
    if [[ "$MODE" == start-image ]]; then
      IMG="$("$DOCKER_BIN" compose config --images aide)"
    else
      export AIDE_VERSION="${AIDE_VERSION:-$(bash scripts/version.sh show)}"
      if [[ -z "${AIDE_COMMIT:-}" ]]; then
        AIDE_COMMIT="$(git rev-parse HEAD 2>/dev/null || printf source-archive)"
        if [[ -n "$(git status --porcelain 2>/dev/null || true)" ]]; then AIDE_COMMIT="${AIDE_COMMIT}-dirty"; fi
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
      if [[ -n "$SRCSHA" && "$cur_label" == "$SRCSHA" && "${AIDE_FORCE_REBUILD:-0}" != 1 ]] && launcher_local_runtime_ready "$DOCKER_BIN" "$IMG"; then
        echo "== 源码哈希未变（${SRCSHA}）→ 快捷路径：跳过构建，直接启动 =="
      else
        launcher_build_source() {
        aide_check_disk build || return $?
        echo "== 源码哈希变化（image=${cur_label:-<无>} → worktree=${SRCSHA}）→ 重新构建（默认不跑全量 test，仅 vet+build）=="
        export AIDE_SRC_SHA="$SRCSHA"
        if [[ "${AIDE_LOCAL_DEPENDENCIES:-1}" != 0 ]]; then
          BUILD_SEED="$IMG"
          # A cleared Docker engine may retain a complete bundle on the host.
          # Recover only dependency layers, never activate that older application.
          if ! launcher_local_runtime_ready "$DOCKER_BIN" "$IMG" && {
            [[ "$("$DOCKER_BIN" image inspect aide:dependencies-v1 --format '{{index .Config.Labels "aide.local-base"}}' 2>/dev/null || true)" != v1 ]] ||
            [[ "$("$DOCKER_BIN" image inspect aide:cad-dependencies-v1 --format '{{index .Config.Labels "aide.cad-runtime"}}' 2>/dev/null || true)" != 8.0.1.1.0 ]];
          }; then
            seed_status=0
            RECOVERED_SEED="$(launcher_local_build_seed "$DOCKER_BIN")" || seed_status=$?
            if (( seed_status == 0 )); then BUILD_SEED="$RECOVERED_SEED";
            elif (( seed_status != 3 )); then
              echo "本地完整包依赖恢复失败；停止，不改用联网构建。" >&2; return "$seed_status"
            fi
          fi
          base_status=0
          LOCAL_BASE="$(bash scripts/prepare-local-base.sh "$BUILD_SEED")" || base_status=$?
          if (( base_status == 0 )); then
            if CAD_BASE="$(bash scripts/prepare-cad-base.sh "$BUILD_SEED")"; then
              export AIDE_CAD_IMAGE="$CAD_BASE"
              export AIDE_DOCKERFILE=Dockerfile.local AIDE_BUILD_NETWORK=none AIDE_DEPENDENCY_IMAGE="$LOCAL_BASE"
            else
              echo "CAD 依赖尚未准备，停止本地构建；按上方命令安装一次后可完全离线。" >&2; return 1
            fi
            echo "== 使用完整本地依赖镜像，应用构建禁止联网 =="
          elif (( base_status != 3 )); then
            echo "本地依赖镜像准备失败；停止，不转为联网构建。" >&2; return "$base_status"
          else
            echo "== 首次缺少完整本地镜像；执行原始构建，完成后可固化本地依赖 =="
          fi
        fi
        "$DOCKER_BIN" compose build aide
        }
        launcher_build_or_release "$DOCKER_BIN" "$IMG" launcher_build_source
      fi
    fi
    launcher_data_prepare "$DOCKER_BIN" "$IMG"
    launcher_env_save
    rm -f .cache/aide-manual-stop
    "$DOCKER_BIN" compose up -d --no-build --pull never
    fi
    bash scripts/message-lan-control.sh start || echo "消息局域网桥未启动；聊天发现请检查 .aide-data/message-lan/bridge.log，工作台仍可使用。"
    if [[ "$(uname -s)" == Darwin ]] && command -v node >/dev/null 2>&1; then
      bash scripts/safari-bridge-control.sh start || echo "Safari 桥接未启动；Aide 其他功能仍可用。"
      bash scripts/computer-bridge-control.sh start || echo "电脑桥接未启动；Aide 其他功能仍可用。"
      bash scripts/headless-browser-control.sh start || echo "无头浏览器未启动；可先运行 bash scripts/headless-browser-control.sh install。"
    fi
    for attempt in {1..60}; do if "$DOCKER_BIN" compose exec -T aide curl -fsSk https://127.0.0.1:8080/healthz >/dev/null 2>&1; then break; fi; sleep 1; done
    if ! CONTAINER_HEALTH="$("$DOCKER_BIN" compose exec -T aide curl -fsSk https://127.0.0.1:8080/healthz)"; then
      echo "AIDE 未就绪，下面是启动错误；浏览器尚未打开。" >&2
      "$DOCKER_BIN" compose logs --tail=30 aide >&2
      exit 1
    fi
    ADDRESS="$("$DOCKER_BIN" compose port aide 8080)" || {
      echo 'AIDE 容器健康，但无法查询宿主端口；浏览器尚未打开。请检查 Compose 端口与网络配置。' >&2
      exit 1
    }
    HOSTPORT=''
    while IFS= read -r binding; do
      if [[ ! "$binding" =~ ^([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+|\[[0-9A-Fa-f:.%]+\]):([0-9]{1,5})$ ]]; then
        echo 'AIDE 容器健康，但没有有效宿主端口；浏览器尚未打开。请检查 Compose ports 和 internal 网络配置。' >&2
        exit 1
      fi
      published_port=$((10#${BASH_REMATCH[2]}))
      if (( published_port < 1 || published_port > 65535 )) || [[ -n "$HOSTPORT" && "$HOSTPORT" != "$published_port" ]]; then
        echo 'AIDE 宿主端口无效或映射不一致；浏览器尚未打开。请检查 Compose ports 配置。' >&2
        exit 1
      fi
      HOSTPORT="$published_port"
    done <<< "$ADDRESS"
    CONTAINER_INSTANCE="$(printf '%s' "$CONTAINER_HEALTH" | sed -n 's/.*"instanceId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    HOST_CERT='.aide-data/storage/data/certs/cert.pem'
    if [[ -z "$CONTAINER_INSTANCE" || ! -r "$HOST_CERT" ]] || ! command -v curl >/dev/null 2>&1; then
      echo 'AIDE 无法核对宿主连接（缺少实例身份、本地证书或 curl）；浏览器尚未打开。容器和数据已保留。' >&2
      exit 1
    fi
    HOST_READY=0
    for attempt in {1..3}; do
      if HOST_HEALTH="$(curl --silent --show-error --fail --proto '=https' --noproxy '*' --connect-timeout 2 --max-time 3 --cacert "$HOST_CERT" "https://localhost:$HOSTPORT/healthz" 2>/dev/null)"; then
        HOST_INSTANCE="$(printf '%s' "$HOST_HEALTH" | sed -n 's/.*"instanceId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        if [[ "$HOST_INSTANCE" == "$CONTAINER_INSTANCE" ]]; then HOST_READY=1; break; fi
      fi
      if (( attempt < 3 )); then sleep 1; fi
    done
    if [[ "$HOST_READY" != 1 ]]; then
      echo 'AIDE 容器健康，但宿主 HTTPS 连接、证书或实例核对未通过；浏览器尚未打开。请检查端口发布、防火墙和启动目录证书。容器和数据已保留。' >&2
      exit 1
    fi
    (nohup bash scripts/watch-port.sh >/dev/null 2>&1 </dev/null &)
    if [[ -f .aide-image && -x scripts/update-agent.sh ]]; then
      (nohup bash scripts/update-agent.sh >/dev/null 2>&1 </dev/null &)
    fi
    # #31 数据分层后令牌位于 /data/auth/access-token；兼容旧路径，读取失败不中断启动
    TOKEN="$("$DOCKER_BIN" compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' || true)"
    if [[ -n "$TOKEN" ]]; then LOGIN_URL="https://localhost:$HOSTPORT/#token=$TOKEN"; else LOGIN_URL="https://localhost:$HOSTPORT/"; fi
    echo "aide 已就绪：https://localhost:$HOSTPORT/ （自签证书，浏览器告警需由用户确认）"
    if [[ -z "$TOKEN" ]]; then echo "未自动读到令牌，已打开登录页；可运行：$DOCKER_BIN compose exec -T aide cat /data/auth/access-token"; fi
    if [[ "${AIDE_OPEN_BROWSER:-1}" == 0 ]]; then :;
    elif [[ "$(uname -s)" == Darwin ]]; then open "$LOGIN_URL";
    elif command -v xdg-open >/dev/null; then xdg-open "$LOGIN_URL";
    else echo "在浏览器打开 $LOGIN_URL ；令牌位于容器 /data/auth/access-token，自签证书浏览器会告警，选择继续。"; fi
    ;;
  status) "$DOCKER_BIN" compose ps ;;
  logs) "$DOCKER_BIN" compose logs --tail=100 aide ;;
  test) "$DOCKER_BIN" run --rm -e AIDE_BUILTIN_PLUGINS= -v "$PROJECT_DIR:/src" -w /src --entrypoint bash aide:local -c 'go test -race -count=1 -timeout=15m ./... && go vet ./...' ;;
  export)
    mkdir -p docker-images
    ARCHIVE="$PROJECT_DIR/docker-images/aide-local.tar.gz"
    trap 'rm -f "$ARCHIVE.partial"' EXIT
    "$DOCKER_BIN" image save aide:local | gzip -1 > "$ARCHIVE.partial"
    mv "$ARCHIVE.partial" "$ARCHIVE"
    record_archive_sum "$ARCHIVE"
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
    cp start.command start.sh start.bat start.ps1 stop.command stop.sh stop.bat stop.ps1 clean-docker-cache.command clean-docker-cache.sh clean-docker-cache.bat clean-docker-cache.ps1 "$BUNDLE/"
    cp scripts/aide-maintenance.sh scripts/aide-maintenance.ps1 "$BUNDLE/scripts/"
    cp scripts/aide.sh scripts/watch-port.sh scripts/watch-port.ps1 scripts/update-agent.sh scripts/update-agent.ps1 scripts/message-lan-control.sh scripts/message-lan-control.ps1 "$BUNDLE/scripts/"
    cp scripts/launcher-data.sh scripts/launcher-data.ps1 scripts/launcher-data.py scripts/prebuilt-image.sh scripts/prebuilt-image.ps1 scripts/resource-guard.sh scripts/resource-guard.ps1 "$BUNDLE/scripts/"
    cp scripts/safari-bridge-control.sh scripts/safari-bridge.js scripts/computer-bridge-control.sh scripts/computer-bridge.js \
      scripts/build-native-computer-bridge.sh scripts/native-computer-bridge.swift scripts/headless-browser-control.sh scripts/headless-browser-bridge.js "$BUNDLE/scripts/"
    mkdir -p "$BUNDLE/scripts/browser-runtime"
    cp scripts/browser-runtime/package.json scripts/browser-runtime/package-lock.json "$BUNDLE/scripts/browser-runtime/"
    cp docker/compose.offline.yaml "$BUNDLE/compose.yaml"
    cp docker/offline.env.example "$BUNDLE/.env.example"
    cp docker/OFFLINE.md "$BUNDLE/README.md"
    cp LICENSE "$BUNDLE/"
    mkdir -p "$BUNDLE/licenses"
    cp vendor/github.com/yuin/goldmark/v2/LICENSE "$BUNDLE/licenses/goldmark.txt"
    mkdir -p "$BUNDLE/licenses/cad"
    cp scripts/cad/licenses/* "$BUNDLE/licenses/cad/"
    printf '%s %s %s v%s\n' "$BUNDLE_TAG" "$IMAGE_ID" "$PLATFORM" "$VERSION" > "$BUNDLE/.aide-image"
    printf 'version=%s\nimage=%s\nplatform=%s\nsrcsha=%s\n' "$VERSION" "$IMAGE_ID" "$PLATFORM" \
      "$("$DOCKER_BIN" image inspect "$IMG" --format '{{index .Config.Labels "aide.srcsha"}}')" > "$BUNDLE/BUILD.txt"
    "$DOCKER_BIN" image tag "$IMAGE_ID" "$BUNDLE_TAG"
    BUNDLE_IMAGES=("$BUNDLE_TAG")
    printf 'cadRuntime=%s\n' "$("$DOCKER_BIN" image inspect "$IMG" --format '{{index .Config.Labels "aide.cad-runtime"}}')" >> "$BUNDLE/BUILD.txt"
    if [[ "$("$DOCKER_BIN" image inspect aide:dependencies-v1 --format '{{index .Config.Labels "aide.local-base"}}' 2>/dev/null || true)" == v1 ]] && \
       [[ "$("$DOCKER_BIN" image inspect aide:dependencies-v1 --format '{{.Os}}/{{.Architecture}}')" == "$PLATFORM" ]]; then
      BUNDLE_IMAGES+=(aide:dependencies-v1)
      printf 'dependencyImage=%s\n' "$("$DOCKER_BIN" image inspect aide:dependencies-v1 --format '{{.Id}}')" >> "$BUNDLE/BUILD.txt"
    fi
    "$DOCKER_BIN" image save -o "$BUNDLE/docker-images/aide-local.tar" "${BUNDLE_IMAGES[@]}"
    (cd "$BUNDLE/docker-images" && shasum -a 256 aide-local.tar > SHA256SUMS)
    COPYFILE_DISABLE=1 tar -czf "$ARCHIVE.partial" -C "$STAGING" aide-offline
    mv "$ARCHIVE.partial" "$ARCHIVE"
    record_archive_sum "$ARCHIVE"
    record_prebuilt_image "$PLATFORM" "$IMAGE_ID" "$(source_sha)" "$ARCHIVE" "v$VERSION"
    echo "离线启动包已导出：$ARCHIVE"
    ;;
  load) "$DOCKER_BIN" image load -i "$PROJECT_DIR/docker-images/aide-local.tar.gz" ;;
  *) echo "用法：scripts/aide.sh {start|start-image|start-bundle|stop|clean-cache|status|logs|test|export|export-bundle|load|source-sha}" >&2; exit 1 ;;
esac
