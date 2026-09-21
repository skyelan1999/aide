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
case "${1:-start}" in
  start)
    "$DOCKER_BIN" compose up -d --build
    for attempt in {1..60}; do if "$DOCKER_BIN" compose exec -T aide curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then break; fi; sleep 1; done
    "$DOCKER_BIN" compose exec -T aide curl -fsS http://127.0.0.1:8080/healthz >/dev/null
    ADDRESS="$("$DOCKER_BIN" compose port aide 8080)"
    TOKEN="$("$DOCKER_BIN" compose exec -T aide cat /data/access-token)"
    echo "aide 已就绪：http://$ADDRESS"
    if [[ "$(uname -s)" == Darwin ]]; then open "http://$ADDRESS/#token=$TOKEN";
    elif command -v xdg-open >/dev/null; then xdg-open "http://$ADDRESS/#token=$TOKEN";
    else echo "在浏览器打开地址，并使用容器 /data/access-token 中的令牌登录。"; fi
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
  *) echo "用法：scripts/aide.sh {start|stop|status|logs|test|export|load}" >&2; exit 1 ;;
esac
