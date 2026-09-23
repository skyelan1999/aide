FROM node:24-bookworm-slim@sha256:0e0ff40c39bc087845bfb27465a0df4ea419520094bc35842ff83dd8cbe6f9b6 AS node
FROM python:3.12-slim-bookworm@sha256:392307d22300de8b5986851a12d9176dfc0fc073e65bf6523ebd7dcbeb23564e AS python
FROM golang:1.26-bookworm@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d AS toolchain
COPY --from=python /usr/local /usr/local
RUN ldconfig
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
FROM toolchain AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
# R09：构建期身份（版本/commit）经 ldflags 注入；版本格式与 version.md 一致（如 0.1.5.0 RC5）
ARG AIDE_VERSION=dev
ARG AIDE_COMMIT=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    go test ./... && go vet ./... && CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X aide/internal/server.buildVersion=${AIDE_VERSION} -X aide/internal/server.buildCommit=${AIDE_COMMIT}" \
    -o /usr/local/bin/aide ./cmd/aide

FROM toolchain AS runtime
COPY --from=build /usr/local/bin/aide /usr/local/bin/aide
RUN groupadd -g 1000 aide && useradd -m -u 1000 -g aide aide \
    && mkdir -p /data /workspace /context /local /home/aide/.cache/go-build /home/aide/go \
    && chown -R aide:aide /data /workspace /context /local /home/aide
ENV AIDE_ADDR=0.0.0.0:8080 AIDE_WORKSPACE=/workspace AIDE_CONTEXT=/context AIDE_DATA=/data
ENV GOCACHE=/home/aide/.cache/go-build GOPATH=/home/aide/go
USER aide
WORKDIR /workspace
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/aide"]
