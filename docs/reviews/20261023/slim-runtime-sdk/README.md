# 运行镜像 / 开发工具拆分验收

基线：`ba11253683d9383c3356387f32e46f4e5c35a3a5`；独立分支 `feat/slim-runtime-sdk-20261023`。本记录为本地候选验证，未合并 main、未升版、未发布、未部署 9999。RC7 发布由另一条任务负责，后续整合必须保留其启动器修复。

## 体积

macOS ARM64 上的 Linux ARM64 Docker VM，非 AMD64 仿真。相同应用源码及逐字节相同的 Aide 二进制，分别用原来的 toolchain 运行阶段与新的 Python slim 运行阶段构建；两次都使用现有 `scripts/package-release-assets.sh` 导出。

| 对象 | 拆分前 | 拆分后 | 减少 |
| --- | ---: | ---: | ---: |
| 本地全量 ZIP | 582,421,638 B | 359,665,139 B | 222,756,499 B / 38.2466% |
| Docker 报告镜像大小 | 2,271,274,818 B | 1,321,503,635 B | 949,771,183 B |

ZIP 与 Docker 展开大小不能混用。候选沿用现有脚本的 RC6 文件名，服务构建身份为 dev/unknown，**不是正式 RC6/RC7 发布物**；不能把此结果当作跨版本正式 Release 的实际下载收益。AMD64 未构建验收。

可选工具包离线镜像为 504,707,691 B（gzip -1），另附 SHA256。安装开发工具的人还需单独下载该包；其共享基础层会在单独的离线文件中重复携带，不应把所有包相加后声称仍节省同样比例。

完整镜像身份、应用二进制 SHA256、全量 ZIP SHA256 与检查边界见 [evidence.json](evidence.json)。运行和离线安装方式见 [开发工具说明](../../../architecture/development-toolbox.md)。

## 已验证

- `docker build ...`：精简 ARM64 运行镜像与相同源码旧运行阶段均成功，构建期 vet 完成。
- `python3 scripts/development-toolbox.py build ...`：可选 SDK 镜像编译成功。
- `test_runtime_assets.py ... --require-lan-bridge`：非 root，Office DOCX 读写、DXF 读写、Node/Python 版本、编译器/npm 缺失、TTS/Node 动态库、12 个宿主辅助程序摘要通过。
- `test_optional_step.py ...`：默认镜像不含 STEP 引擎/插件，DXF/STL 资源保留。
- 隔离 CA 验证的健康、鉴权、配置、更新槽、零插件 API 冒烟通过；不调用付费模型。
- SDK 在 network=none、只读根目录、显式工作区下实际运行 Go，编译并运行 C，执行 npm/node/Python；导出 SHA256 和 `docker load` 验证通过。
- 所有 18 个运行镜像层、15,017 项审计通过：无插件载荷、Go/GCC/npm 编译器层。
- 16 个 CI 合约测试及 9 个源码种子测试通过。最终 quick 137 项检查全部通过且输入指纹稳定。
- 浏览器只验收运行环境标签组件：使用实际 HTML/CSS，显示 Python / Node.js / Git，未进行完整工作台、付费模型或本地 TTS 模型发声验收。组件标签和两台临时静态服务器已关闭。

Go race/vet 最终通过：1,187 项服务端注册测试、24/24 个互斥分片、其他 Go 包 race 和 vet 均完成，退出码 0；原始简要输出见 [go-race-vet.txt](go-race-vet.txt)。初次 Go 全量执行因后续输入改变主动终止，不作为通过证据；最终快照对比了 5,566 个受控非文档输入，与当前内容一致。

本次不自动将助理命令转发到 SDK，不开放 Docker socket，不改 API Key、数据目录、生产端口或现有插件安装。后续正式发布前仍需在整合后的 main 上完成发布门禁和双架构构建。
