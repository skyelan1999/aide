# 离线 sherpa 二进制包放置处（可选）

空气隙构建：将与 SHERPA_VERSION 和目标架构匹配的上游包放在此处，再设置 `--build-arg SHERPA_OFFLINE=1`：

- ARM64：`sherpa-onnx-v<version>-linux-aarch64-shared-cpu.tar.bz2`
- AMD64：`sherpa-onnx-v<version>-linux-x64-shared.tar.bz2`

构建仅选择目标架构的准确文件名。发行门禁验证离线 TTS 可执行文件及其动态库，缺失则拒绝发布。
