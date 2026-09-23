#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
network="aide-source-test-$$"
server="aide-source-test-$$"
certdir="$(mktemp -d)"
cleanup() {
 docker rm -f "$server" >/dev/null 2>&1 || true
 docker network rm "$network" >/dev/null 2>&1 || true
 rm -rf "$certdir"
}
trap cleanup EXIT
docker build -t aide-source-fixtures:test scripts/fixtures/sources
docker network create --internal "$network" >/dev/null
docker run -d --name "$server" --network "$network" --network-alias aide-source-fixtures aide-source-fixtures:test >/dev/null
ready=false
for attempt in {1..40}; do
 if docker exec "$server" curl -fsS http://127.0.0.1:8080/root.txt >/dev/null 2>&1; then ready=true; break; fi
 sleep 1
done
if [[ "$ready" != true ]]; then docker logs "$server"; exit 1; fi
docker cp "$server:/certs/ca.crt" "$certdir/ca.crt" >/dev/null
docker run --rm --network "$network" -e AIDE_SOURCE_FIXTURE_HOST=aide-source-fixtures \
 -e CURL_CA_BUNDLE=/fixtures/ca.crt -e GOFLAGS=-buildvcs=false \
 -v "$root:/src:ro" -v "$certdir:/fixtures:ro" -w /src --entrypoint bash aide:local \
 -c 'mkdir -p /home/aide/.ssh; go test -race -v ./internal/server -run TestSourcesLiveProtocols -count=1 -timeout=180s'
