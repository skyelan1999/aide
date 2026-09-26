# python 基础镜像默认引用本地 tag，避免受限网络下按 digest 拉取 metadata 卡死。
# 本机 tag 镜像与下述 digest 内容一致（从既有构建产物提取，见 HANDOVER）。
# 发布构建可重新固定：--build-arg PYTHON_BASE=python:3.12-slim-bookworm@sha256:392307d22300de8b5986851a12d9176dfc0fc073e65bf6523ebd7dcbeb23564e
ARG PYTHON_BASE=python:3.12-slim-bookworm
FROM node:24-bookworm-slim@sha256:0e0ff40c39bc087845bfb27465a0df4ea419520094bc35842ff83dd8cbe6f9b6 AS node
FROM ${PYTHON_BASE} AS python
FROM golang:1.26-bookworm@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d AS toolchain
COPY --from=python /usr/local /usr/local
RUN ldconfig
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

# ── build stage：编译 aide 二进制 ──────────────────────────────────────────────
# 热路径优化（#43）：默认不再跑全量 go test。日常 start 走层缓存，只做 vet+build；
# 全量测试移到发布门禁——release/CI 必须 --build-arg AIDE_RUN_TESTS=1。
FROM toolchain AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn
COPY go.mod ./
COPY go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY internal ./internal
# R09：构建期身份（版本/commit）经 ldflags 注入；版本格式与 version.md 一致（如 0.1.5.0 RC5）
ARG AIDE_VERSION=dev
ARG AIDE_COMMIT=unknown
# AIDE_RUN_TESTS=1 时才跑全量 go test（发布门禁强制）；默认 0 跳过，仅 vet+build。
# echo 把参数值写进命令文本，确保切换 AIDE_RUN_TESTS 必然失效该层缓存（否则 =0 缓存会被 =1 误命中而跳过测试）。
# go build 缓存经 cache mount 跨构建复用，增量编译秒级。
ARG AIDE_RUN_TESTS=0
RUN --mount=type=cache,target=/root/.cache/go-build \
    echo "[build] AIDE_RUN_TESTS=${AIDE_RUN_TESTS}" && \
    if [ "${AIDE_RUN_TESTS}" = "1" ]; then echo "[build] 发布门禁：跑全量 go test"; go test -mod=vendor -count=1 ./...; fi && \
    go vet -mod=vendor ./... && \
    CGO_ENABLED=0 go build -mod=vendor -trimpath \
    -ldflags "-s -w -X 'aide/internal/server.buildVersion=${AIDE_VERSION}' -X 'aide/internal/server.buildCommit=${AIDE_COMMIT}'" \
    -o /usr/local/bin/aide ./cmd/aide

# ── runtime stage：运行时镜像 ──────────────────────────────────────────────────
# 层缓存优化（#46）：把「稳定层」（系统用户 + pip 依赖）放在 COPY 业务二进制【之前】，
# 把随每次代码/前端改动而变的 COPY 二进制放到最后。这样改 Go/前端只失效最末 COPY 层，
# pip 层稳定 CACHED、不重复联网下载；运行期镜像自包含、绝不联网 pip。
FROM toolchain AS runtime

# 1) 系统用户与目录（root 执行，早于 USER aide）——稳定层
RUN groupadd -g 1000 aide && useradd -m -u 1000 -g aide aide \
    && mkdir -p /data /workspace /context /local /home/aide/.cache/go-build /home/aide/go \
    && chown -R aide:aide /data /workspace /context /local /home/aide

# 2) Office 文档解析依赖（开源免费）——稳定层，pin 版本可复现
#    PIP_INDEX_URL 默认国内镜像加速，CI/发布可 --build-arg 覆盖为官方源。
#    pip 下载缓存经 cache mount 跨构建复用。
#    离线/air-gap 首次构建：先在联网机跑 scripts/prebuild-wheels.sh 预下载到 docker/wheels/，
#    再 --build-arg PIP_OFFLINE=1 用 --no-index --find-links=/wheels 纯本地安装。
ARG PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple/
ARG PIP_OFFLINE=0
COPY docker/wheels /wheels
RUN --mount=type=cache,target=/root/.cache/pip \
    echo "[runtime] PIP_OFFLINE=${PIP_OFFLINE} index=${PIP_INDEX_URL}" && \
    if [ "${PIP_OFFLINE}" = "1" ]; then \
      echo "[runtime] 离线模式：从 /wheels 安装（--no-index）"; \
      pip install --no-index --find-links=/wheels \
        python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4; \
    else \
      echo "[runtime] 在线模式：从 ${PIP_INDEX_URL} 安装"; \
      pip install --retries 5 --timeout 60 -i ${PIP_INDEX_URL} \
        python-docx==1.2.0 openpyxl==3.1.5 python-pptx==1.0.2 ezdxf==1.4.4; \
    fi

# 2.5) 本地离线 TTS：sherpa-onnx 预编译 CPU 二进制（#44，Apache-2.0，无 cgo）——稳定层
#    单个 shared-cpu 包已含 bin/sherpa-onnx-offline-tts + lib/libonnxruntime.so 等运行库（自包含）。
#    模型仍外置 /data/tts（不进镜像控体积）。在线构建从 GitHub release 下载；
#    离线/air-gap 构建先在联网机把 shared-cpu tar.bz2 放进 docker/sherpa/，再 --build-arg SHERPA_OFFLINE=1
#    （与 docker/wheels 同一套离线约定）。体积增量约 +30MB 压缩（解压后 ~70MB）。
ARG SHERPA_VERSION=1.13.8
ARG SHERPA_OFFLINE=0
COPY docker/sherpa /sherpa-pkg
RUN set -ux; \
    arch="$(uname -m)"; \
    if [ "$arch" = "aarch64" ]; then sarch="aarch64"; else sarch="x64"; fi; \
    cd /tmp; \
    sherpa_ok=0; \
    if [ "${SHERPA_OFFLINE}" = "1" ]; then \
      echo "[runtime] sherpa 离线模式：使用 docker/sherpa 本地包"; \
      if ls /sherpa-pkg/sherpa-onnx-*-shared-cpu.tar.bz2 >/dev/null 2>&1; then \
        cp /sherpa-pkg/sherpa-onnx-*-shared-cpu.tar.bz2 ./pkg.tar.bz2 && sherpa_ok=1; \
      else echo "[runtime] WARN: docker/sherpa/ 中未找到离线包，跳过 sherpa"; fi; \
    else \
      echo "[runtime] sherpa 在线模式：下载 v${SHERPA_VERSION} (${sarch})"; \
      if curl -fSL --retry 3 --retry-delay 2 --connect-timeout 10 -o pkg.tar.bz2 \
        "https://github.com/k2-fsa/sherpa-onnx/releases/download/v${SHERPA_VERSION}/sherpa-onnx-v${SHERPA_VERSION}-linux-${sarch}-shared-cpu.tar.bz2" 2>/dev/null; then \
        sherpa_ok=1; \
      else echo "[runtime] WARN: sherpa 下载失败（网络不可达），跳过离线TTS，应用仍正常运行"; fi; \
    fi; \
    if [ "$sherpa_ok" = "1" ]; then \
      apt-get update -qq 2>/dev/null && apt-get install -y -qq bzip2 >/dev/null 2>&1 || true; \
      mkdir -p /sherpa-x; \
      if tar xjf pkg.tar.bz2 -C /sherpa-x 2>/dev/null; then \
        sherpa_dir="$(find /sherpa-x -maxdepth 1 -type d -name 'sherpa-onnx-*' 2>/dev/null | head -1)"; \
        if [ -n "$sherpa_dir" ] && [ -f "$sherpa_dir/bin/sherpa-onnx-offline-tts" ]; then \
          cp "$sherpa_dir/bin/sherpa-onnx-offline-tts" /usr/local/bin/sherpa-onnx-offline-tts; \
          cp -a "$sherpa_dir/lib/." /usr/local/lib/ 2>/dev/null || true; \
          ldconfig 2>/dev/null || true; \
          chmod +x /usr/local/bin/sherpa-onnx-offline-tts; \
          echo "[runtime] sherpa 安装成功"; \
        else echo "[runtime] WARN: sherpa 包内容异常，跳过"; fi; \
      else echo "[runtime] WARN: sherpa 解压失败，跳过"; fi; \
      rm -rf /sherpa-x /tmp/pkg.tar.bz2; \
    fi; \
    rm -rf /var/lib/apt/lists/*

# 2.6) Office 工具脚本（#63）：aide 经 os/exec 调用（docx_structure/comments 等）。
#      查找顺序：$AIDE_OFFICE_SCRIPTS → /workspace/scripts/office（dev 挂仓）→ 本目录。
COPY scripts/office /opt/aide/office-scripts

# 3) 业务二进制（随每次代码/前端改动变化）——最易失效的层放在最后
COPY --from=build /usr/local/bin/aide /usr/local/bin/aide

# 4) 元数据：源码哈希 label 供 aide.sh start 快捷路径比对；版本/环境/用户/健康检查
#    AIDE_SRC_SHA 由 aide.sh start 计算 cmd+internal+go.mod/go.sum/vendor 哈希后注入。
ARG AIDE_SRC_SHA=unknown
LABEL aide.srcsha="${AIDE_SRC_SHA}"
ENV AIDE_ADDR=0.0.0.0:8080 AIDE_WORKSPACE=/workspace AIDE_CONTEXT=/context AIDE_DATA=/data
ENV GOCACHE=/home/aide/.cache/go-build GOPATH=/home/aide/go
USER aide
WORKDIR /workspace
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 CMD curl -fsSk https://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/aide"]
