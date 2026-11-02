# 本地资产验收镜像选择

问题：[719](https://github.com/skyelan1999/aide/issues/719)。main `8b9f73fa` 的完整 Go 验收已通过：1,590 个注册项、1,512 通过、78 明确跳过、24 个完整分片，其余 Go 包／平台／vet 也通过。但 full 总收据为失败，因为两个资产检查使用了本机不存在的 `aide:local`；其中 `test_runtime_assets.py` 不读取 `AIDE_TEST_IMAGE`，与 optional STEP 检查不一致。`--network=none` 只限制容器网络，不能阻止 Docker 在启动前拉取缺失镜像。

修复使两入口一致：显式位置参数优先，其次非空 `AIDE_TEST_IMAGE`，最后保留旧默认标签。资产检查均设置 `--pull=never`；没有改 Docker registry、代理配置、生产容器、资源／功能断言或产品代码。新的无 Docker mock 回归覆盖两个入口的未设／空环境、指定环境、显式参数覆盖和禁用隐式拉取，纳入 quick。

本机单独验证使用现有的运行依赖镜像 `sha256:e4b3040c61c64835833bfc2e28101b0da263842ea51efd1d588df20a75f3e84b`，资产检查以普通 UID 1000 读取 32 个文件并完成 DOCX 生成／读回；optional STEP 排除检查通过。该镜像只是本地测试依赖，**不是本次新版本运行镜像的同源验收证明**；正式 tagged Delivery 必须构建并验证新版本的 ARM64／AMD64 运行包。

原始失败的 full 收据及完整 Go 通过报告保存在本机忽略目录 `.agent-state/release-ci-hardening-20261101/post-audit-full/`，不会改写为通过。修复后使用明确镜像参数重新运行 full，结果和 CI／集成记录回填[任务账本](../../tasks/runtime-test-image-20261102.json)。
